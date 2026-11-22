package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"curetmdbanime/internal/config"
	"curetmdbanime/internal/logger"
	"github.com/gin-gonic/gin"
)

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

func streamTestServer(t *testing.T, heartbeat time.Duration) *httptest.Server {
	t.Helper()
	setupManagementLogger(t)
	router := gin.New()
	router.GET("/stream", managementAuth, func(c *gin.Context) {
		serveLogStream(c, heartbeat, 500*time.Millisecond)
	})
	server := httptest.NewUnstartedServer(router)
	server.Config.WriteTimeout = 30 * time.Millisecond
	server.Start()
	t.Cleanup(func() { logger.StopStreams(); server.Close() })
	return server
}

type testSSEEvent struct {
	Name    string
	ID      uint64
	Data    string
	Comment string
}

type testSSEReader struct {
	reader *bufio.Reader
	body   io.ReadCloser
}

func openTestSSE(t *testing.T, url, lastID string) *testSSEReader {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" || response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("unexpected SSE response: %s %v", response.Status, response.Header)
	}
	return &testSSEReader{reader: bufio.NewReader(response.Body), body: response.Body}
}

func (r *testSSEReader) read() (testSSEEvent, error) {
	var event testSSEEvent
	for {
		line, err := r.reader.ReadString('\n')
		if err != nil {
			return event, err
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			return event, nil
		}
		if strings.HasPrefix(line, ":") {
			event.Comment = strings.TrimSpace(strings.TrimPrefix(line, ":"))
			continue
		}
		key, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch key {
		case "event":
			event.Name = value
		case "id":
			event.ID, _ = strconv.ParseUint(value, 10, 64)
		case "data":
			event.Data = value
		}
	}
}

func nextSSE(t *testing.T, reader *testSSEReader) testSSEEvent {
	t.Helper()
	type result struct {
		event testSSEEvent
		err   error
	}
	ready := make(chan result, 1)
	go func() {
		event, err := reader.read()
		ready <- result{event, err}
	}()
	select {
	case got := <-ready:
		if got.err != nil {
			t.Fatal(got.err)
		}
		return got.event
	case <-time.After(3 * time.Second):
		_ = reader.body.Close()
		t.Fatal("SSE event timed out")
		return testSSEEvent{}
	}
}

func nextLog(t *testing.T, reader *testSSEReader) testSSEEvent {
	t.Helper()
	for {
		event := nextSSE(t, reader)
		if event.Name == "log" {
			return event
		}
	}
}

func TestLogStreamLiveFilterRedactionAndHeartbeat(t *testing.T) {
	server := streamTestServer(t, 50*time.Millisecond)
	logger.Error("TMDB old history")
	reader := openTestSSE(t, server.URL+"/stream?level=error&keyword=tmdb", "")
	if event := nextSSE(t, reader); event.Name != "cursor" || event.ID != 1 {
		t.Fatal(event)
	}
	logger.Info("TMDB filtered")
	// 超过服务器全局 WriteTimeout，流仍应能发送心跳和新日志。
	time.Sleep(100 * time.Millisecond)
	if event := nextSSE(t, reader); event.Comment != "heartbeat" {
		t.Fatal(event)
	}
	if event := nextSSE(t, reader); event.Name != "cursor" || event.ID != 2 {
		t.Fatal(event)
	}
	logger.Error("tmdb 番剧 api_key=secret")
	event := nextLog(t, reader)
	var entry logger.Entry
	if err := json.Unmarshal([]byte(event.Data), &entry); err != nil {
		t.Fatal(err)
	}
	if event.ID != 3 || entry.ID != 3 || strings.Contains(entry.Message, "secret") || !strings.Contains(entry.Message, "[REDACTED]") {
		t.Fatal(event)
	}
}

func TestLogStreamReplayAndReconnect(t *testing.T) {
	server := streamTestServer(t, 50*time.Millisecond)
	for _, message := range []string{"keep one", "other", "KEEP three", "other", "keep five"} {
		logger.Info("%s", message)
	}
	reader := openTestSSE(t, server.URL+"/stream?after=0&limit=1&keyword=keep", "")
	if event := nextSSE(t, reader); event.Name != "cursor" || event.ID != 0 {
		t.Fatal(event)
	}
	logger.Info("keep live")
	for _, id := range []uint64{1, 3, 5, 6} {
		if event := nextLog(t, reader); event.ID != id {
			t.Fatalf("got %+v, want ID %d", event, id)
		}
	}
	_ = reader.body.Close()
	reconnected := openTestSSE(t, server.URL+"/stream?keyword=keep", "3")
	if event := nextSSE(t, reconnected); event.Name != "cursor" || event.ID != 3 {
		t.Fatal(event)
	}
	for _, id := range []uint64{5, 6} {
		if event := nextLog(t, reconnected); event.ID != id {
			t.Fatal(event)
		}
	}
	// 查询游标优先于 Last-Event-ID，即使 header 无效也不影响有效的 after。
	override := openTestSSE(t, server.URL+"/stream?after=5", "invalid")
	if event := nextSSE(t, override); event.ID != 5 {
		t.Fatal(event)
	}
	if event := nextLog(t, override); event.ID != 6 {
		t.Fatal(event)
	}
}

func TestLogStreamCursorReset(t *testing.T) {
	server := streamTestServer(t, 50*time.Millisecond)
	logger.Info("retained")
	reader := openTestSSE(t, server.URL+"/stream?after=999", "")
	if event := nextSSE(t, reader); event.Name != "reset" || event.ID != 0 || !strings.Contains(event.Data, "cursor_ahead") {
		t.Fatal(event)
	}
	if event := nextLog(t, reader); event.ID != 1 {
		t.Fatal(event)
	}
}

func TestLogStreamLimitCancellationAndShutdown(t *testing.T) {
	server := streamTestServer(t, 50*time.Millisecond)
	var readers []*testSSEReader
	for i := 0; i < logger.MaxStreams; i++ {
		reader := openTestSSE(t, server.URL+"/stream", "")
		_ = nextSSE(t, reader)
		readers = append(readers, reader)
	}
	response, err := http.Get(server.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 429 {
		t.Fatal(response.StatusCode)
	}
	_ = readers[0].body.Close()
	deadline := time.Now().Add(time.Second)
	for logger.Status().StreamCount == logger.MaxStreams && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if logger.Status().StreamCount != logger.MaxStreams-1 {
		t.Fatal("cancelled request leaked a subscription")
	}
	logger.StopStreams()
	if logger.Status().StreamCount != 0 {
		t.Fatal("shutdown leaked subscriptions")
	}
	response, err = http.Get(server.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 503 {
		t.Fatal(response.StatusCode)
	}
}

func TestLogStreamPrunedHistoryReset(t *testing.T) {
	dir := setupManagementLogger(t)
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	entry := logger.Entry{ID: 100, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Level: "info", Message: "oldest retained"}
	data := fmt.Sprintf("%s [INFO] [id=%d] %s\n", entry.Timestamp, entry.ID, entry.Message)
	path := filepath.Join(dir, "logs", "curetmdb.log")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := logger.Initialize(dir, 1, 2); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	RegisterManagementRoutes(router)
	server := httptest.NewServer(router)
	t.Cleanup(func() { logger.StopStreams(); server.Close() })
	reader := openTestSSE(t, server.URL+"/management/logs/stream?after=0", "")
	if event := nextSSE(t, reader); event.Name != "reset" || event.ID != 99 || !strings.Contains(event.Data, "history_pruned") {
		t.Fatal(event)
	}
	if event := nextLog(t, reader); event.ID != 100 {
		t.Fatal(event)
	}
}

// 用真实 HTTP 连接与缓慢的传输 writer 验证背压和写入截止时间。
type delayedStreamWriter struct {
	http.ResponseWriter
	delay time.Duration
}

func (w delayedStreamWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w delayedStreamWriter) Write(data []byte) (int, error) {
	if bytes.Contains(data, []byte("event: log")) {
		time.Sleep(w.delay)
	}
	return w.ResponseWriter.Write(data)
}

func delayedStreamServer(t *testing.T, delay, writeTimeout time.Duration) *httptest.Server {
	t.Helper()
	setupManagementLogger(t)
	router := gin.New()
	router.GET("/stream", func(c *gin.Context) { serveLogStream(c, time.Second, writeTimeout) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		router.ServeHTTP(delayedStreamWriter{ResponseWriter: w, delay: delay}, r)
	}))
	t.Cleanup(func() { logger.StopStreams(); server.Close() })
	return server
}

func TestLogStreamSlowConsumerDoesNotBlockWrites(t *testing.T) {
	server := delayedStreamServer(t, 20*time.Millisecond, 500*time.Millisecond)
	reader := openTestSSE(t, server.URL+"/stream", "")
	_ = nextSSE(t, reader)
	start := time.Now()
	const count = logger.SubscriptionQueue * 2
	for i := 0; i < count; i++ {
		logger.Info("slow consumer record %d", i)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("slow stream blocked application logging")
	}
	deadline := time.Now().Add(time.Second)
	for logger.Status().StreamCount != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if logger.Status().StreamCount != 0 {
		t.Fatal("slow consumer not disconnected")
	}
	if result, err := logger.Query(context.Background(), logger.QueryOptions{Limit: 1}); err != nil || result.NextAfter != count {
		t.Fatalf("logging stopped: %+v %v", result, err)
	}
}

func TestLogStreamWriteDeadline(t *testing.T) {
	server := delayedStreamServer(t, 100*time.Millisecond, 20*time.Millisecond)
	reader := openTestSSE(t, server.URL+"/stream", "")
	_ = nextSSE(t, reader)
	logger.Info("write deadline")
	finished := make(chan error, 1)
	go func() {
		_, err := reader.read()
		finished <- err
	}()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("stream write exceeded deadline but succeeded")
		}
	case <-time.After(time.Second):
		_ = reader.body.Close()
		t.Fatal("blocked writer not interrupted")
	}
}

func TestLogStreamAuthenticationAndInvalidInput(t *testing.T) {
	server := streamTestServer(t, 50*time.Millisecond)
	for _, path := range []string{"?after=", "?after=-1", "?after=x", "?before=10", "?after=1&before=2", "?limit=0", "?level=unknown", "?after=1&after=2"} {
		response, err := http.Get(server.URL + "/stream" + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != 400 {
			t.Fatalf("%s: %d", path, response.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", server.URL+"/stream", nil)
	req.Header.Set("Last-Event-ID", "bad")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatal(response.StatusCode)
	}
	// 此时没有活动请求，设置令牌再验证鉴权。
	config.AppSettings.ManagementToken = "secret"
	for _, header := range []string{"", "Bearer wrong", "Basic secret", "bearer secret"} {
		req, _ := http.NewRequest("GET", server.URL+"/stream", nil)
		req.Header.Set("Authorization", header)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != 401 {
			t.Fatalf("%q: %s", header, response.Status)
		}
	}
	req, _ = http.NewRequest("GET", server.URL+"/stream", nil)
	req.Header.Set("Authorization", "Bearer secret")
	response, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatal(fmt.Sprint(response.Status))
	}
	_ = response.Body.Close()
}
