package logger

import (
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// formatEntry 将记录写成人可读的固定前缀文本；每条记录只占一个物理行。
// 换行、反斜杠及控制字符转义，读取后恢复原消息，避免伪造日志前缀。
func formatEntry(entry Entry) []byte {
	line := make([]byte, 0, len(entry.Timestamp)+len(entry.Level)+len(entry.Message)+32)
	line = append(line, entry.Timestamp...)
	line = append(line, " ["...)
	line = append(line, strings.ToUpper(entry.Level)...)
	line = append(line, "] [id="...)
	line = strconv.AppendUint(line, entry.ID, 10)
	line = append(line, "] "...)
	const hex = "0123456789abcdef"
	for i := 0; i < len(entry.Message); i++ {
		char := entry.Message[i]
		switch char {
		case '\\':
			line = append(line, '\\', '\\')
		case '\n':
			line = append(line, '\\', 'n')
		case '\r':
			line = append(line, '\\', 'r')
		case '\t':
			line = append(line, '\\', 't')
		default:
			if char < 0x20 || char == 0x7f {
				line = append(line, '\\', 'x', hex[char>>4], hex[char&0xf])
			} else {
				line = append(line, char)
			}
		}
	}
	return append(line, '\n')
}

func parseEntry(line []byte) (Entry, error) {
	text := strings.TrimSuffix(string(line), "\n")
	timestamp, rest, ok := strings.Cut(text, " ")
	if !ok {
		return Entry{}, errors.New("invalid log prefix")
	}
	if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
		return Entry{}, errors.New("invalid log timestamp")
	}
	level, rest, ok := strings.Cut(rest, "] ")
	if !ok || !strings.HasPrefix(level, "[") || !validLevel(level[1:]) {
		return Entry{}, errors.New("invalid log level")
	}
	idField, message, ok := strings.Cut(rest, "] ")
	if !ok || !strings.HasPrefix(idField, "[id=") {
		return Entry{}, errors.New("invalid log ID prefix")
	}
	id, err := strconv.ParseUint(idField[len("[id="):], 10, 64)
	if err != nil || id == 0 {
		return Entry{}, errors.New("invalid log ID")
	}
	message, err = unescapeMessage(message)
	if err != nil {
		return Entry{}, err
	}
	return Entry{ID: id, Timestamp: timestamp, Level: strings.ToLower(level[1:]), Message: message}, nil
}

func unescapeMessage(message string) (string, error) {
	if !utf8.ValidString(message) {
		return "", errors.New("invalid log message UTF-8")
	}
	if !strings.ContainsRune(message, '\\') {
		return message, nil
	}
	var decoded strings.Builder
	decoded.Grow(len(message))
	for i := 0; i < len(message); i++ {
		if message[i] != '\\' {
			decoded.WriteByte(message[i])
			continue
		}
		i++
		if i == len(message) {
			return "", errors.New("incomplete log message escape")
		}
		switch message[i] {
		case '\\':
			decoded.WriteByte('\\')
		case 'n':
			decoded.WriteByte('\n')
		case 'r':
			decoded.WriteByte('\r')
		case 't':
			decoded.WriteByte('\t')
		case 'x':
			if i+2 >= len(message) {
				return "", errors.New("incomplete log message escape")
			}
			char, err := strconv.ParseUint(message[i+1:i+3], 16, 8)
			if err != nil {
				return "", errors.New("invalid log message escape")
			}
			decoded.WriteByte(byte(char))
			i += 2
		default:
			return "", errors.New("invalid log message escape")
		}
	}
	result := decoded.String()
	if !utf8.ValidString(result) {
		return "", errors.New("invalid log message UTF-8")
	}
	return result, nil
}
