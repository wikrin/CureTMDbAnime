package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"curetmdbanime/internal/config"
	"curetmdbanime/internal/logger"
	"github.com/gin-gonic/gin"
)

func TestManagementRoutes(t *testing.T) {
	if err := logger.Initialize(t.TempDir(), 1, 2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	old := config.AppSettings
	defer func() { config.AppSettings = old }()
	config.AppSettings.ManagementToken = "test-management-secret"
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterManagementRoutes(router)
	request := func(path, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	health := request("/management/health", "")
	if health.Code != 200 || !strings.Contains(health.Body.String(), `"service":"curetmdbanime"`) {
		t.Fatal(health.Body.String())
	}
	for _, token := range []string{"", "wrong"} {
		if w := request("/management/logs", token); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	logger.Info("test-management-log")
	if w := request("/management/logs?limit=10", "test-management-secret"); w.Code != 200 || !strings.Contains(w.Body.String(), "test-management-log") {
		t.Fatal(w.Body.String())
	}
	if w := request("/management/logs?limit=9999", "test-management-secret"); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestManagementLogBackwardPagination(t *testing.T) {
	setupManagementLogger(t)
	router := gin.New()
	RegisterManagementRoutes(router)
	request := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	for id := 1; id <= 450; id++ {
		if id%5 == 0 {
			logger.Error("TMDB 番剧 record %d", id)
		} else {
			logger.Info("other record %d", id)
		}
	}
	for _, tc := range []struct {
		query       string
		first, last uint64
		count       int
		older, more bool
	}{
		{"", 251, 450, 200, true, false},
		{"?before=251", 51, 250, 200, true, true},
		{"?before=51", 1, 50, 50, false, false},
		{"?limit=2000", 1, 450, 450, false, false},
		{"?before=405&limit=10&level=ERROR&keyword=tmdb", 355, 400, 10, true, true},
	} {
		response := request("/management/logs" + tc.query)
		var page logger.QueryResult
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &page) != nil || len(page.Items) != tc.count || page.Items[0].ID != tc.first || page.Items[len(page.Items)-1].ID != tc.last || page.NextBefore != tc.first || page.HasOlder != tc.older || page.HasMore != tc.more {
			t.Fatalf("%s: status=%d %s", tc.query, response.Code, response.Body.String())
		}
	}
	for _, query := range []string{"?before=", "?before=0", "?before=-1", "?before=bad", "?before=18446744073709551616", "?before=1&before=2", "?after=0&before=1"} {
		if response := request("/management/logs" + query); response.Code != 400 {
			t.Fatalf("%s: %d", query, response.Code)
		}
	}
	var empty logger.QueryResult
	response := request("/management/logs?before=1")
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &empty) != nil || len(empty.Items) != 0 || empty.HasOlder || empty.NextBefore != 0 {
		t.Fatal(response.Body.String())
	}
	config.AppSettings.ManagementToken = "history-secret"
	if response := request("/management/logs?before=251"); response.Code != 401 {
		t.Fatal("history query bypassed management auth")
	}
}

func TestOptionalManagementAuthAndLogQuery(t *testing.T) {
	setupManagementLogger(t)
	router := gin.New()
	RegisterManagementRoutes(router)
	request := func(path, authorization string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Authorization", authorization)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	logger.Info("first")
	logger.Error("TMDB 番剧")
	logger.Info("tmdb 番剧")
	logger.Error("last")
	for _, tc := range []struct {
		path   string
		id     uint64
		cursor uint64
		more   bool
	}{
		{"/management/logs?limit=1", 4, 4, false},
		{"/management/logs?after=0&limit=1", 1, 1, true},
		{"/management/logs?after=0&limit=1&level=ERROR&keyword=tmdb", 2, 4, false},
		{"/management/logs?keyword=" + url.QueryEscape("番剧") + "&level=error", 2, 4, false},
	} {
		response := request(tc.path, "")
		var result logger.QueryResult
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Items) != 1 || result.Items[0].ID != tc.id || result.NextAfter != tc.cursor || result.HasMore != tc.more {
			t.Fatalf("%s: %d %s", tc.path, response.Code, response.Body.String())
		}
	}
	for _, path := range []string{"?after=", "?after=-1", "?after=bad", "?limit=0", "?limit=2001", "?level=bad", "?keyword=" + strings.Repeat("x", 4097)} {
		if response := request("/management/logs"+path, ""); response.Code != 400 {
			t.Fatalf("%s: %d", path, response.Code)
		}
	}
	var health struct {
		Capabilities map[string]bool      `json:"capabilities"`
		AuthRequired bool                 `json:"management_auth_required"`
		Logging      logger.StorageStatus `json:"logging"`
	}
	if err := json.Unmarshal(request("/management/health", "").Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.AuthRequired || !health.Capabilities["persistent_logs"] || !health.Logging.Available {
		t.Fatal(health)
	}
	config.AppSettings.ManagementToken = "optional-secret"
	for _, path := range []string{"/management/logs", "/management/upstreams"} {
		for _, header := range []string{"", "Bearer wrong", "Basic optional-secret"} {
			if response := request(path, header); response.Code != 401 {
				t.Fatalf("%s %q: %d", path, header, response.Code)
			}
		}
	}
	if response := request("/management/health", ""); response.Code != 200 || !strings.Contains(response.Body.String(), `"management_auth_required":true`) {
		t.Fatal(response.Body.String())
	}
	if response := request("/management/logs", "Bearer optional-secret"); response.Code != 200 {
		t.Fatal(response.Code)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	if response := request("/management/logs", "Bearer optional-secret"); response.Code != 503 {
		t.Fatal("unavailable logger disguised as a successful empty query")
	}
}

func TestUpstreamStatusDistinguishesAuthentication(t *testing.T) {
	for _, code := range []int{200, 401, 403, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
		result := probeUpstream(context.Background(), "test", server.URL, "")
		server.Close()
		if !result.Reachable || result.StatusCode != code || result.Available != (code == 200) {
			t.Fatalf("%+v", result)
		}
	}
}

func TestProbeUsesConfiguredTargets(t *testing.T) {
	old := config.AppSettings
	defer func() { config.AppSettings = old }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	config.AppSettings.ManagementToken = "secret"
	config.AppSettings.TmdbAPIURL = server.URL
	config.AppSettings.TVDBAPIURL = server.URL
	config.AppSettings.BangumiAPIURL = server.URL
	config.AppSettings.Proxy = ""
	router := gin.New()
	RegisterManagementRoutes(router)
	for _, token := range []string{"secret", ""} {
		config.AppSettings.ManagementToken = token
		req := httptest.NewRequest("GET", "/management/upstreams", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var result struct {
			Items []upstreamStatus `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || len(result.Items) != 3 {
			t.Fatal(w.Body.String())
		}
		for _, item := range result.Items {
			if !item.Available {
				t.Fatal(item)
			}
		}
	}
	probeGate <- struct{}{}
	defer func() { <-probeGate }()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/management/upstreams", nil))
	if w.Code != 429 {
		t.Fatal("probe concurrency limit lost")
	}
}

func setupManagementLogger(t *testing.T) string {
	t.Helper()
	old := config.AppSettings
	config.AppSettings.ManagementToken = ""
	t.Cleanup(func() { config.AppSettings = old })
	dir := t.TempDir()
	if err := logger.Initialize(dir, 1, 2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	return dir
}
