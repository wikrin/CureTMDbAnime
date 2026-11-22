package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"curetmdbanime/internal/config"
	"curetmdbanime/internal/logger"
	"github.com/gin-gonic/gin"
)

func TestApplicationLogScopeAndNoManagementFeedback(t *testing.T) {
	setupManagementLogger(t)
	oldWriter, oldDebug := gin.DefaultWriter, config.AppSettings.Debug
	gin.DefaultWriter = io.Discard
	logger.SetDebug(true)
	t.Cleanup(func() {
		gin.DefaultWriter = oldWriter
		logger.SetDebug(oldDebug)
	})
	router := SetupRouter()
	router.GET("/test-business", func(c *gin.Context) { c.Status(200) })
	logger.Info("feedback seed")
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/test-business?api_key=must-not-be-persisted", nil))
	for _, path := range []string{"/management/health", "/management/logs"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != 200 {
			t.Fatal(response.Code)
		}
	}
	server := httptest.NewServer(router)
	t.Cleanup(func() { logger.StopStreams(); server.Close() })
	reader := openTestSSE(t, server.URL+"/management/logs/stream", "")
	_ = nextSSE(t, reader)
	_ = reader.body.Close()
	result, err := logger.Query(context.Background(), logger.QueryOptions{Limit: 10})
	if err != nil || len(result.Items) != 2 || result.Items[0].Level != "info" || result.Items[1].Level != "debug" {
		t.Fatalf("unexpected persisted sources or feedback: %+v %v", result, err)
	}
	for _, entry := range result.Items {
		if strings.Contains(entry.Message, "[GIN]") || strings.Contains(entry.Message, "must-not-be-persisted") || strings.Contains(entry.Message, "/management/") {
			t.Fatal(entry)
		}
	}
}

func TestAccessLogsSkipSuccessfulManagementRequests(t *testing.T) {
	if err := logger.Initialize(t.TempDir(), 1, 2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	oldSettings, oldWriter := config.AppSettings, gin.DefaultWriter
	defer func() {
		config.AppSettings = oldSettings
		gin.DefaultWriter = oldWriter
	}()
	config.AppSettings.ManagementToken = "test-secret"
	var output bytes.Buffer
	gin.DefaultWriter = &output
	router := SetupRouter()
	router.GET("/test-business", func(c *gin.Context) { c.Status(200) })

	for _, test := range []struct {
		path   string
		token  string
		status int
		logged bool
	}{
		{"/management/health", "", 200, false},
		{"/management/logs", "test-secret", 200, false},
		{"/management/logs", "wrong", 401, true},
		{"/test-business?api_key=must-not-be-logged", "", 200, true},
	} {
		output.Reset()
		req := httptest.NewRequest("GET", test.path, nil)
		if test.token != "" {
			req.Header.Set("Authorization", "Bearer "+test.token)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != test.status {
			t.Fatalf("%s: status %d, want %d", test.path, response.Code, test.status)
		}
		if logged := output.Len() > 0; logged != test.logged {
			t.Fatalf("%s: unexpected access log %q", test.path, output.String())
		}
		if strings.Contains(output.String(), "must-not-be-logged") || strings.Contains(output.String(), "test-secret") {
			t.Fatalf("access log leaked credentials: %q", output.String())
		}
	}
}
