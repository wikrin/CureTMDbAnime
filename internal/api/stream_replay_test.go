package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"curetmdbanime/internal/logger"
	"github.com/gin-gonic/gin"
)

func TestLogStreamLargeSingleRecordPagesThenLive(t *testing.T) {
	dir := setupManagementLogger(t)
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	const count = 6000
	var data strings.Builder
	for id := 1; id <= count; id++ {
		fmt.Fprintf(&data, "2000-01-01T00:00:00Z [INFO] [id=%d] TMDB 番剧 record %d\n", id, id)
	}
	if err := os.WriteFile(filepath.Join(dir, "logs", "curetmdb.log"), []byte(data.String()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := logger.Initialize(dir, 1, 2); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	RegisterManagementRoutes(router)
	server := httptest.NewServer(router)
	t.Cleanup(func() { logger.StopStreams(); server.Close() })
	reader := openTestSSE(t, server.URL+"/management/logs/stream?after=0&limit=1&keyword=tmdb", "")
	if event := nextSSE(t, reader); event.Name != "cursor" || event.ID != 0 {
		t.Fatal(event)
	}
	// 在历史读取期间追加，必须在全部补发记录之后只收到一次。
	logger.Info("TMDB live after replay boundary api_key=must-be-redacted")
	for id := uint64(1); id <= count+1; id++ {
		event := nextLog(t, reader)
		var entry logger.Entry
		if event.ID != id || json.Unmarshal([]byte(event.Data), &entry) != nil || entry.ID != id {
			t.Fatalf("replay/live gap or duplicate: got %+v, want ID %d", event, id)
		}
		if id == count+1 && (strings.Contains(entry.Message, "must-be-redacted") || !strings.Contains(entry.Message, "[REDACTED]")) {
			t.Fatalf("live message lost redaction: %+v", entry)
		}
	}
	_ = reader.body.Close()
}
