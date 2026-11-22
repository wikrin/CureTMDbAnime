package logger

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"
)

func TestTextFormatReadableAndRoundTrip(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "fatal"} {
		for _, message := range []string{
			"获取上游失败: TMDB ID=123",
			`response: {"message":"not found"} [id=99]`,
			"first\nsecond\r\nthird\tcolumn",
			`C:\new\folder literal\n`,
			"\x00\x01\x1b[31m\x7f",
			"", "  leading and trailing  ",
		} {
			entry := Entry{ID: math.MaxUint64, Timestamp: "2026-07-05T10:20:30.123Z", Level: level, Message: message}
			line := formatEntry(entry)
			if strings.Count(string(line), "\n") != 1 || !strings.HasPrefix(string(line), entry.Timestamp+" ["+strings.ToUpper(level)+"] [id=") {
				t.Fatalf("invalid physical record: %q", line)
			}
			got, err := parseEntry(line)
			if err != nil || got != entry {
				t.Fatalf("round trip: got %+v, want %+v, err=%v", got, entry, err)
			}
		}
	}
	entry := Entry{ID: 42, Timestamp: "2026-07-05T10:20:30.123Z", Level: "error", Message: "获取上游失败"}
	if got := string(formatEntry(entry)); got != "2026-07-05T10:20:30.123Z [ERROR] [id=42] 获取上游失败\n" {
		t.Fatal(got)
	}
}

func TestTextParseRejectsMalformedRecords(t *testing.T) {
	prefix := "2026-07-05T10:20:30.123Z [INFO] [id=1] "
	for _, line := range []string{
		"missing prefix\n",
		"invalid-time [INFO] [id=1] message\n",
		"2026-07-05T10:20:30Z [UNKNOWN] [id=1] message\n",
		"2026-07-05T10:20:30Z [INFO] [id=0] message\n",
		"2026-07-05T10:20:30Z [INFO] [id=-1] message\n",
		"2026-07-05T10:20:30Z [INFO] [id=18446744073709551616] message\n",
		"2026-07-05T10:20:30Z [INFO] no-id message\n",
		prefix + "invalid\\q\n",
		prefix + "incomplete\\\n",
		prefix + "incomplete\\x0\n",
		prefix + "invalid\\xgg\n",
		prefix + "invalid UTF8\\xff\n",
	} {
		if _, err := parseEntry([]byte(line)); err == nil {
			t.Fatalf("accepted malformed record: %q", line)
		}
	}
}

func TestTextPersistenceAndMultilineQuery(t *testing.T) {
	store := openTestStore(t, 1<<20, 2)
	message := "番剧处理\nsecond\nthird\t" + `literal\n {"message":"failed"}`
	entry := appendTest(t, store, "error", message)
	path := store.files[0].path
	if !strings.HasSuffix(path, ".log") {
		t.Fatal(path)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "[ERROR] [id=1] 番剧处理\\nsecond") || strings.Count(string(data), "\n") != 1 {
		t.Fatalf("not readable single-line text: %q %v", data, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(store.dir, 1<<20, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after := uint64(0)
	page, err := reopened.Query(context.Background(), QueryOptions{After: &after, Limit: 1, Filter: Filter{Keyword: "second\nthird"}})
	if err != nil || len(page.Items) != 1 || page.Items[0] != entry {
		t.Fatalf("text query lost original message: %+v %v", page, err)
	}
}

func TestLogFileNamesOnlyAcceptLumberjackTextFiles(t *testing.T) {
	for _, name := range []string{fileName, "curetmdb-2026-07-05T10-20-30.123.log"} {
		if _, ok := logFileTime(name); !ok {
			t.Fatalf("text log file rejected: %s", name)
		}
	}
	for _, name := range []string{"curetmdb-00000000000000000001.log", "curetmdb.jsonl", "curetmdb.log.bak", "curetmdb-invalid-time.log", "other.log"} {
		if _, ok := logFileTime(name); ok {
			t.Fatalf("unowned file accepted: %s", name)
		}
	}
}

func FuzzTextEntryRoundTrip(f *testing.F) {
	for _, message := range []string{"番剧处理", "first\nsecond", `path\new\file`, "\x00\x1b", "invalid\xff"} {
		f.Add(message)
	}
	f.Fuzz(func(t *testing.T, message string) {
		entry := Entry{ID: 42, Timestamp: "2026-07-05T10:20:30.123Z", Level: "info", Message: sanitize(message)}
		line := formatEntry(entry)
		got, err := parseEntry(line)
		if err != nil || got != entry || strings.Count(string(line), "\n") != 1 {
			t.Fatalf("text round trip failed: %q %v", line, err)
		}
	})
}
