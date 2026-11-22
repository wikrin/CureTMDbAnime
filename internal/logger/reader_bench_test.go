package logger

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func benchmarkReaderSnapshot(b *testing.B) *Snapshot {
	b.Helper()
	dir := b.TempDir()
	var data []byte
	for id := uint64(1); id <= 6000; id++ {
		entry := Entry{ID: id, Timestamp: "2000-01-01T00:00:00Z", Level: "info", Message: "fixture message for replay benchmark"}
		data = append(data, formatEntry(entry)...)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), data, 0600); err != nil {
		b.Fatal(err)
	}
	store, err := OpenStore(dir, mib, 2)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	snapshot, err := store.Snapshot()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(snapshot.Close)
	b.SetBytes(int64(len(data)))
	return snapshot
}

// 对应 SSE 的实际补发路径：所有页面共享一个顺序读取器。
func BenchmarkReplayPages(b *testing.B) {
	for _, limit := range []int{1, 200, 2000} {
		b.Run(fmt.Sprintf("limit=%d", limit), func(b *testing.B) {
			snapshot := benchmarkReaderSnapshot(b)
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				after := uint64(0)
				replay, err := snapshot.NewReplay(QueryOptions{After: &after, Limit: limit})
				if err != nil {
					b.Fatal(err)
				}
				for {
					page, err := replay.Next(context.Background())
					if err != nil {
						b.Fatal(err)
					}
					if !page.HasMore {
						break
					}
				}
			}
		})
	}
}

// 对应独立的 HTTP after 查询：每页重新定位，但不扫描旧的整段前缀。
func BenchmarkForwardPagination(b *testing.B) {
	for _, limit := range []int{1, 200, 2000} {
		b.Run(fmt.Sprintf("limit=%d", limit), func(b *testing.B) {
			snapshot := benchmarkReaderSnapshot(b)
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				cursor := uint64(0)
				for {
					page, err := snapshot.Query(context.Background(), QueryOptions{After: &cursor, Limit: limit})
					if err != nil {
						b.Fatal(err)
					}
					cursor = page.NextAfter
					if !page.HasMore {
						break
					}
				}
			}
		})
	}
}

func BenchmarkLatestPage(b *testing.B) {
	snapshot := benchmarkReaderSnapshot(b)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := snapshot.Query(context.Background(), QueryOptions{Limit: DefaultLimit}); err != nil {
			b.Fatal(err)
		}
	}
}
