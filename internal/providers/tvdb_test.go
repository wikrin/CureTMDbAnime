package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"curetmdbanime/internal/config"
)

func TestTVDBClientGetSeriesEpisodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login":
			if r.Method != http.MethodPost {
				t.Errorf("login method = %s, want POST", r.Method)
			}
			w.Write([]byte(`{"data":{"token":"test-token"}}`))
		case "/series/456/episodes/official/eng":
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Errorf("authorization = %q, want Bearer test-token", got)
			}
			if got := r.URL.Query().Get("page"); got != "0" {
				t.Errorf("page = %q, want 0", got)
			}
			w.Write([]byte(`{"data":{"series":{"id":456},"episodes":[{"id":1,"name":"One","seasonNumber":1,"number":1},{"id":2,"name":"Two","seasonNumber":1,"number":2},{"id":3,"name":"Three","seasonNumber":2,"number":1}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	original := config.AppSettings
	config.AppSettings.TVDBAPIKey = "test-key"
	config.AppSettings.TVDBPIN = ""
	config.AppSettings.TVDBAPIURL = server.URL
	defer func() { config.AppSettings = original }()

	entry, err := NewTVDBClient().GetSeriesEpisodes(context.Background(), 456)
	if err != nil {
		t.Fatalf("GetSeasonInfo() error = %v", err)
	}
	if entry == nil || len(entry.Seasons) != 2 {
		t.Fatalf("GetSeasonInfo() = %#v, want two seasons", entry)
	}
	if entry.Seasons[0].EpisodeCount != 2 || entry.Seasons[1].EpisodeCount != 1 {
		t.Fatalf("episode counts = %d, %d; want 2, 1", entry.Seasons[0].EpisodeCount, entry.Seasons[1].EpisodeCount)
	}
}
