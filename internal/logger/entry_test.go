package logger

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeCredentials(t *testing.T) {
	for _, message := range []string{
		"request api_key=secret&token=secret access_token=secret",
		"Authorization: Bearer secret",
		"https://user:secret@example.test/path",
		`{"apikey":"secret","token":"secret","password":"sec\"ret","Authorization":"Bearer secret"}`,
	} {
		redacted := sanitize(message)
		if strings.Contains(redacted, "secret") || strings.Contains(redacted, `sec\"ret`) || !strings.Contains(redacted, "[REDACTED]") {
			t.Fatalf("not redacted: %q", redacted)
		}
	}
}

func TestSanitizeUTF8AndLength(t *testing.T) {
	for _, message := range []string{strings.Repeat("番", 2000), strings.Repeat("x", 4095) + "番", "invalid\xffUTF8"} {
		got := sanitize(message)
		if !utf8.ValidString(got) || len(got) > maxMessageBytes {
			t.Fatalf("invalid message: bytes=%d valid=%v", len(got), utf8.ValidString(got))
		}
	}
}

func TestFilter(t *testing.T) {
	entry := Entry{Level: "error", Message: "TMDB ID=123 番剧处理失败"}
	for _, tc := range []struct {
		filter Filter
		want   bool
	}{
		{Filter{}, true},
		{Filter{Level: "ERROR", Keyword: "tmdb id=123"}, true},
		{Filter{Keyword: "番剧"}, true},
		{Filter{Keyword: "ID=.*"}, false},
		{Filter{Level: "info", Keyword: "番剧"}, false},
	} {
		if got := tc.filter.Matches(entry); got != tc.want {
			t.Fatalf("filter %+v = %v, want %v", tc.filter, got, tc.want)
		}
		if got := tc.filter.Compile().Matches(entry); got != tc.want {
			t.Fatalf("compiled filter %+v = %v, want %v", tc.filter, got, tc.want)
		}
	}
}

func TestCompiledFilterCapturesOriginalKeyword(t *testing.T) {
	filter := Filter{Level: "ERROR", Keyword: "TMDB 番剧"}
	matcher := filter.Compile()
	filter.Keyword = "different keyword"
	if !matcher.Matches(Entry{Level: "error", Message: "tmdb 番剧处理失败"}) {
		t.Fatal("compiled filter lost its original case-insensitive keyword")
	}
}
