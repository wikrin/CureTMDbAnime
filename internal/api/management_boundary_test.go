package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"curetmdbanime/internal/config"
	"curetmdbanime/internal/logger"
	"github.com/gin-gonic/gin"
)

func TestManagementNamespaceNeverProxies(t *testing.T) {
	oldSettings, oldWriter := config.AppSettings, gin.DefaultWriter
	t.Cleanup(func() {
		config.AppSettings = oldSettings
		gin.DefaultWriter = oldWriter
	})
	gin.DefaultWriter = io.Discard
	type forwardedRequest struct{ path, authorization string }
	forwarded := make(chan forwardedRequest, 20)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded <- forwardedRequest{r.URL.Path, r.Header.Get("Authorization")}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	config.AppSettings.TmdbAPIURL = upstream.URL
	config.AppSettings.Proxy = ""
	router := SetupRouter()
	for _, token := range []string{"", "namespace-secret"} {
		config.AppSettings.ManagementToken = token
		for _, tc := range []struct {
			method, path string
			status       int
		}{
			{http.MethodGet, "/management", http.StatusNotFound},
			{http.MethodGet, "/management/", http.StatusNotFound},
			{http.MethodGet, "/management/not-supported", http.StatusNotFound},
			{http.MethodGet, "/management/logs/not-supported", http.StatusNotFound},
			{http.MethodGet, "//management/not-supported", http.StatusNotFound},
			{http.MethodGet, "/3/../management/not-supported", http.StatusNotFound},
			{http.MethodGet, "/management/../3/movie/1", http.StatusNotFound},
			{http.MethodGet, "/management%2fnot-supported", http.StatusNotFound},
			{http.MethodPost, "/management/logs", http.StatusMethodNotAllowed},
			{http.MethodHead, "/management/health", http.StatusMethodNotAllowed},
			{http.MethodOptions, "/management/upstreams", http.StatusMethodNotAllowed},
			{http.MethodDelete, "/management/logs/stream", http.StatusMethodNotAllowed},
		} {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer namespace-secret")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("%s %s: status=%d, want %d", tc.method, tc.path, w.Code, tc.status)
			}
			if tc.status == http.StatusMethodNotAllowed && w.Header().Get("Allow") != http.MethodGet {
				t.Fatalf("%s %s: missing Allow: GET", tc.method, tc.path)
			}
			select {
			case got := <-forwarded:
				t.Fatalf("management request reached upstream: %+v", got)
			default:
			}
		}
	}
	// 管理命名空间的保护不能破坏普通 TMDB Bearer 代理，也不能误伤相似前缀。
	for _, path := range []string{"/3/movie/1", "/management-other"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer upstream-access-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("ordinary proxy %s: status=%d", path, w.Code)
		}
		got := <-forwarded
		if got.path != path || got.authorization != "Bearer upstream-access-token" {
			t.Fatalf("ordinary proxy changed request: %+v", got)
		}
	}
}

func TestManagementRejectsMalformedQueryEncoding(t *testing.T) {
	setupManagementLogger(t)
	logger.Info("literal;&+ 番剧")
	router := gin.New()
	RegisterManagementRoutes(router)
	server := httptest.NewServer(router)
	t.Cleanup(func() { logger.StopStreams(); server.Close() })
	client := &http.Client{Timeout: 2 * time.Second}
	for _, path := range []string{"/management/logs", "/management/logs/stream"} {
		for _, query := range []string{
			"after=%zz", "before=%zz", "limit=%zz", "level=%zz", "keyword=%zz",
			"after=0&keyword=%zz", "after=1;limit=1", "after=0&unused=%zz",
		} {
			response, err := client.Get(server.URL + path + "?" + query)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil || response.StatusCode != http.StatusBadRequest {
				t.Fatalf("%s?%s: status=%d body=%s err=%v", path, query, response.StatusCode, body, err)
			}
			if logger.Status().StreamCount != 0 {
				t.Fatal("invalid query created a stream subscription")
			}
		}
	}
	response, err := client.Get(server.URL + "/management/logs?after=0&keyword=" + url.QueryEscape("literal;&+ 番剧"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), "番剧") {
		t.Fatalf("valid encoded keyword rejected: status=%d body=%s err=%v", response.StatusCode, body, err)
	}
}

func TestProbeStripsUpstreamURLCredentials(t *testing.T) {
	headers := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	for _, user := range []*url.Userinfo{
		url.User("secret-as-username"),
		url.UserPassword("test-user", "secret@password"),
		url.UserPassword("", "secret-password"),
	} {
		address, _ := url.Parse(server.URL)
		address.User = user
		result := probeUpstream(t.Context(), "test", address.String(), "")
		if !result.Reachable || result.Available || result.StatusCode != http.StatusUnauthorized {
			t.Fatalf("unexpected unauthenticated probe status: %+v", result)
		}
		got := <-headers
		if got.Get("Authorization") != "" || got.Get("Cookie") != "" || got.Get("User-Agent") != "CureTMDbAnime-health/1" {
			t.Fatalf("probe sent unexpected headers: %v", got)
		}
	}
}

func TestProbeKeepsCredentialsLimitedToConfiguredProxy(t *testing.T) {
	headers := make(chan http.Header, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	proxyURL.User = url.UserPassword("proxy-user", "proxy-password")
	result := probeUpstream(t.Context(), "test", "http://upstream-user:upstream-password@upstream.invalid/test", proxyURL.String())
	if !result.Available {
		t.Fatal(result)
	}
	got := <-headers
	if got.Get("Authorization") != "" || got.Get("Proxy-Authorization") == "" {
		t.Fatalf("proxy credentials leaked or were discarded: %v", got)
	}
}
