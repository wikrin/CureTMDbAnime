package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSizePruningPrecedesBackupCountPruning(t *testing.T) {
	for _, tc := range []struct {
		name         string
		largeCurrent bool
		maxFiles     int
		wantFirst    uint64
		wantCount    int
	}{
		{"large current", true, 3, 1, 2},
		{"large backup", false, 3, 1, 3},
		{"shrink both limits", true, 2, 2, 1},
		{"single file", true, 1, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name string, data []byte) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			entryFor := func(id uint64, message string) Entry {
				return Entry{ID: id, Timestamp: "2000-01-01T00:00:00Z", Level: "info", Message: message}
			}
			for id := uint64(1); id <= 2; id++ {
				name := fmt.Sprintf("curetmdb-2000-01-0%dT00-00-00.000.log", id)
				write(name, formatEntry(entryFor(id, "small retained record")))
			}
			var large []byte
			message := strings.Repeat("x", maxMessageBytes)
			for id := uint64(3); id <= 272; id++ {
				large = append(large, formatEntry(entryFor(id, message))...)
			}
			lastID := uint64(272)
			if tc.largeCurrent {
				write(fileName, large)
			} else {
				write("curetmdb-2000-01-03T00-00-00.000.log", large)
				lastID++
				write(fileName, formatEntry(entryFor(lastID, "small current record")))
			}
			store, err := OpenStore(dir, mib, tc.maxFiles)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			after := uint64(0)
			page := queryTest(t, store, QueryOptions{After: &after, Limit: 10})
			if len(page.Items) != tc.wantCount || (tc.wantCount > 0 && page.Items[0].ID != tc.wantFirst) {
				t.Fatalf("fitting records were unnecessarily pruned: %+v", page)
			}
			if len(store.files) != min(3, tc.maxFiles) || store.sequence != lastID {
				t.Fatalf("unexpected retention/cursor: files=%d sequence=%d", len(store.files), store.sequence)
			}
			// 淘汰包含最新 ID 的超大文件后，空当前文件重启也不能复用已分配 ID。
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = OpenStore(dir, mib, tc.maxFiles)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if entry := appendTest(t, store, "info", "after size pruning and restart"); entry.ID != lastID+1 {
				t.Fatalf("pruning reused a persisted ID: %+v", entry)
			}
		})
	}
}
