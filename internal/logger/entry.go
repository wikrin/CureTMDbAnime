package logger

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Entry 是文件、日志查询和实时流共用的结构化记录。
type Entry struct {
	ID        uint64 `json:"id"`
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

const maxMessageBytes = 4096

var (
	credentials       = regexp.MustCompile(`(?i)(api_key|apikey|access_token|refresh_token|management_token|token|authorization|password|proxy)=([^\s&]+)`)
	urlCredentials    = regexp.MustCompile(`(?i)(https?://)[^\s/@]+:[^\s/@]+@`)
	bearerCredentials = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+)[^\s,;]+`)
	jsonCredentials   = regexp.MustCompile(`(?i)("(?:api_key|apikey|access_token|refresh_token|management_token|token|authorization|password|proxy)"\s*:\s*)"(?:\\.|[^"\\])*"`)
)

func sanitize(message string) string {
	message = jsonCredentials.ReplaceAllString(message, `${1}"[REDACTED]"`)
	message = bearerCredentials.ReplaceAllString(message, "${1}[REDACTED]")
	message = credentials.ReplaceAllString(message, "${1}=[REDACTED]")
	message = urlCredentials.ReplaceAllString(message, "${1}[REDACTED]@")
	message = strings.ToValidUTF8(message, "\uFFFD")
	if len(message) > maxMessageBytes {
		message = message[:maxMessageBytes]
		for !utf8.ValidString(message) {
			message = message[:len(message)-1]
		}
	}
	return message
}

func validLevel(level string) bool {
	switch strings.ToLower(level) {
	case "debug", "info", "warn", "error", "fatal":
		return true
	default:
		return false
	}
}

// Filter 匹配脱敏后的正文；关键词按字面子串匹配，不支持正则。
type Filter struct {
	Level   string
	Keyword string
}

func (f Filter) Matches(entry Entry) bool {
	return f.Compile().Matches(entry)
}

// Matcher 是预处理后的只读筛选器，可在一次查询或流连接中复用。
// 不持有日志正文；关键词的小写转换只执行一次。
type Matcher struct {
	level, keyword string
}

// Compile 预处理关键词，不改变原 Filter。
func (f Filter) Compile() Matcher {
	return Matcher{level: f.Level, keyword: strings.ToLower(f.Keyword)}
}

func (m Matcher) Matches(entry Entry) bool {
	return (m.level == "" || strings.EqualFold(m.level, entry.Level)) &&
		(m.keyword == "" || strings.Contains(strings.ToLower(entry.Message), m.keyword))
}
