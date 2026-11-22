package api

import (
	"context"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"curetmdbanime/internal/config"
	"curetmdbanime/internal/logger"
	"github.com/gin-gonic/gin"
)

var startedAt = time.Now()
var probeGate = make(chan struct{}, 1)

type upstreamStatus struct {
	Name       string `json:"name"`
	Reachable  bool   `json:"reachable"`
	Available  bool   `json:"available"`
	StatusCode int    `json:"status_code"`
	LatencyMS  int64  `json:"latency_ms"`
	Message    string `json:"message"`
}

func RegisterManagementRoutes(router *gin.Engine) {
	router.GET("/management/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"service": "curetmdbanime", "api_version": 1, "status": "healthy",
			"version": config.Version, "uptime_seconds": int64(time.Since(startedAt).Seconds()),
			"capabilities":             gin.H{"logs": true, "upstreams": true, "persistent_logs": true, "log_stream": true},
			"management_auth_required": config.AppSettings.ManagementToken != "",
			"logging":                  logger.Status(),
		})
	})
	group := router.Group("/management", managementAuth)
	group.GET("/logs", func(c *gin.Context) {
		options, err := parseLogQuery(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
			return
		}
		result, err := logger.Query(c.Request.Context(), options)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": "persistent logs unavailable"})
			return
		}
		c.JSON(http.StatusOK, result)
	})
	group.GET("/logs/stream", logStream)
	group.GET("/upstreams", func(c *gin.Context) {
		select {
		case probeGate <- struct{}{}:
			defer func() { <-probeGate }()
		default:
			c.JSON(429, gin.H{"message": "probe in progress"})
			return
		}
		cfg := config.AppSettings
		targets := []struct {
			name, address string
			proxy         bool
		}{
			{"tmdb", strings.TrimRight(cfg.TmdbAPIURL, "/") + "/3/configuration", true},
			{"tvdb", strings.TrimRight(cfg.TVDBAPIURL, "/") + "/languages", true},
			{"bangumi", strings.TrimRight(cfg.BangumiAPIURL, "/") + "/v0/subjects/1", cfg.BangumiUseProxy},
		}
		results := make([]upstreamStatus, len(targets))
		var wg sync.WaitGroup
		for i, target := range targets {
			wg.Add(1)
			go func(i int, name, address string, useProxy bool) {
				defer wg.Done()
				proxy := ""
				if useProxy {
					proxy = cfg.Proxy
				}
				results[i] = probeUpstream(c.Request.Context(), name, address, proxy)
			}(i, target.name, target.address, target.proxy)
		}
		wg.Wait()
		c.JSON(200, gin.H{"checked_at": time.Now().UTC().Format(time.RFC3339), "items": results})
	})
}

func managementAuth(c *gin.Context) {
	token := config.AppSettings.ManagementToken
	if token == "" {
		c.Next()
		return
	}
	header := c.GetHeader("Authorization")
	provided := strings.TrimPrefix(header, "Bearer ")
	if !strings.HasPrefix(header, "Bearer ") || subtle.ConstantTimeCompare([]byte(token), []byte(provided)) != 1 {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "management token required"})
		return
	}
	c.Next()
}

// 不携带上游凭据：401/403 代表网络可达但尚未验证业务/认证可用。
func probeUpstream(parent context.Context, name, address, proxy string) upstreamStatus {
	result := upstreamStatus{Name: name}
	begin := time.Now()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	if proxy != "" {
		parsed, err := url.Parse(proxy)
		if err != nil || parsed.Host == "" {
			result.Message = "invalid proxy configuration"
			return result
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		result.Message = "invalid upstream address"
		return result
	}
	// Client.Do 会根据 URL.User 自动添加 Basic Authorization，探测必须去除它。
	// PROXY 的用户信息仍由 Transport 使用，只用于所配置的代理。
	req.URL.User = nil
	req.Header.Set("User-Agent", "CureTMDbAnime-health/1")
	client := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	result.LatencyMS = time.Since(begin).Milliseconds()
	if err != nil {
		result.Message = "connection failed or timed out"
		return result
	}
	defer response.Body.Close()
	result.Reachable = true
	result.StatusCode = response.StatusCode
	result.Available = response.StatusCode >= 200 && response.StatusCode < 300
	switch {
	case result.Available:
		result.Message = "endpoint available (not a full business check)"
	case response.StatusCode == 401 || response.StatusCode == 403:
		result.Message = "reachable; upstream authentication/permission not verified"
	default:
		result.Message = "reachable; upstream returned non-success status"
	}
	return result
}
