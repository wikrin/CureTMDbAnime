package logger

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func backwardFixture(t *testing.T, count, split uint64, entryFor func(uint64) Entry) *Store {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, first, last uint64) {
		var data []byte
		for id := first; id <= last; id++ {
			entry := Entry{ID: id, Timestamp: "2026-07-05T10:20:30Z", Level: "info", Message: fmt.Sprintf("record %d", id)}
			if entryFor != nil {
				entry = entryFor(id)
				entry.ID, entry.Timestamp = id, "2026-07-05T10:20:30Z"
			}
			data = append(data, formatEntry(entry)...)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if split > 0 {
		write("curetmdb-2026-07-05T10-20-30.123.log", 1, split)
	}
	write(fileName, split+1, count)
	store, err := OpenStore(dir, mib, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestBackwardPaginationAcrossFiles(t *testing.T) {
	store := backwardFixture(t, 1100, 500, nil)
	page := queryTest(t, store, QueryOptions{Limit: DefaultLimit})
	if len(page.Items) != 200 || page.Items[0].ID != 901 || page.Items[199].ID != 1100 || page.NextBefore != 901 || !page.HasOlder || page.HasMore || page.NextAfter != 1100 {
		t.Fatalf("latest page: %+v", page)
	}
	// 查询更早记录时，新日志不能挤入页面或改变已拿到的 before 游标。
	appendTest(t, store, "info", "new live record")
	seen := make(map[uint64]bool)
	for _, entry := range page.Items {
		seen[entry.ID] = true
	}
	for page.HasOlder {
		before := page.NextBefore
		page = queryTest(t, store, QueryOptions{Before: &before, Limit: DefaultLimit})
		if page.NextBefore >= before || page.HasMore != page.HasOlder || page.NextAfter != before-1 {
			t.Fatalf("cursor did not move backward: %+v before=%d", page, before)
		}
		for i, entry := range page.Items {
			if entry.ID >= before || seen[entry.ID] || (i > 0 && entry.ID <= page.Items[i-1].ID) {
				t.Fatalf("duplicate/out-of-order entry: %+v before=%d", entry, before)
			}
			seen[entry.ID] = true
		}
	}
	if len(seen) != 1100 || page.NextBefore != 1 || len(page.Items) != 100 {
		t.Fatalf("history incomplete: count=%d last page=%+v", len(seen), page)
	}
	before := uint64(1)
	empty := queryTest(t, store, QueryOptions{Before: &before, Limit: DefaultLimit})
	if len(empty.Items) != 0 || empty.HasOlder || empty.HasMore || empty.NextBefore != 0 || empty.Gap {
		t.Fatal(empty)
	}
}

func TestBackwardPaginationWithFilters(t *testing.T) {
	store := backwardFixture(t, 60, 30, func(id uint64) Entry {
		entry := Entry{Level: "info", Message: "other"}
		if id%3 == 0 {
			entry.Level = "error"
		}
		if id%6 == 0 {
			entry.Message = "TMDB 番剧 failed"
		}
		if id == 59 {
			entry.Message = "unique"
		}
		return entry
	})
	options := QueryOptions{Limit: 3, Filter: Filter{Level: "ERROR", Keyword: "tmdb 番剧"}}
	for _, expected := range [][]uint64{{48, 54, 60}, {30, 36, 42}, {12, 18, 24}, {6}} {
		page := queryTest(t, store, options)
		if len(page.Items) != len(expected) || page.HasOlder != (expected[0] != 6) {
			t.Fatalf("filtered page: %+v", page)
		}
		for i, id := range expected {
			if page.Items[i].ID != id {
				t.Fatal(page.Items)
			}
		}
		before := page.NextBefore
		options.Before = &before
	}
	unique := queryTest(t, store, QueryOptions{Limit: 10, Filter: Filter{Keyword: "unique"}})
	if len(unique.Items) != 1 || unique.NextBefore != 59 || unique.HasOlder {
		t.Fatal("older nonmatching records counted as matching history")
	}
	before := uint64(59)
	empty := queryTest(t, store, QueryOptions{Before: &before, Limit: 10, Filter: Filter{Keyword: "unique"}})
	if len(empty.Items) != 0 || empty.HasOlder || empty.NextBefore != 0 {
		t.Fatal(empty)
	}
}

func TestBackwardPaginationSurvivesRestart(t *testing.T) {
	store := backwardFixture(t, 500, 200, nil)
	latest := queryTest(t, store, QueryOptions{Limit: 100})
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(store.dir, mib, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	before := latest.NextBefore
	page := queryTest(t, reopened, QueryOptions{Before: &before, Limit: 100})
	if len(page.Items) != 100 || page.Items[0].ID != 301 || page.Items[99].ID != 400 || page.NextBefore != 301 || !page.HasOlder {
		t.Fatal(page)
	}
}

func TestBackwardPaginationPrunedCursorDoesNotReturnNewerRecords(t *testing.T) {
	store := backwardFixture(t, 500, 200, nil)
	before := uint64(201)
	if err := os.Remove(filepath.Join(store.dir, "curetmdb-2026-07-05T10-20-30.123.log")); err != nil {
		t.Fatal(err)
	}
	page := queryTest(t, store, QueryOptions{Before: &before, Limit: 100})
	if len(page.Items) != 0 || !page.Gap || page.Reset != "history_pruned" || page.HasOlder || page.NextBefore != 0 {
		t.Fatalf("pruned cursor reset to newer history: %+v", page)
	}
	before = 301
	page = queryTest(t, store, QueryOptions{Before: &before, Limit: 100})
	if len(page.Items) != 100 || page.Items[0].ID != 201 || page.HasOlder {
		t.Fatal(page)
	}
}

func TestBackwardCursorBoundariesAndValidation(t *testing.T) {
	store := backwardFixture(t, 10, 0, nil)
	before := uint64(math.MaxUint64)
	page := queryTest(t, store, QueryOptions{Before: &before, Limit: 5})
	if len(page.Items) != 5 || page.Items[0].ID != 6 || page.NextBefore != 6 || !page.HasOlder || !page.Gap || page.Reset != "cursor_ahead" || page.NextAfter != 10 {
		t.Fatal(page)
	}
	after, zero := uint64(0), uint64(0)
	for _, options := range []QueryOptions{
		{Before: &zero, Limit: 1},
		{Before: &before, After: &after, Limit: 1},
		{Before: &before, Limit: 0},
		{Before: &before, Limit: MaxLimit + 1},
	} {
		if _, err := store.Query(context.Background(), options); err == nil {
			t.Fatal("invalid backward query accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Query(ctx, QueryOptions{Before: &before, Limit: 1}); err == nil {
		t.Fatal("cancelled backward query succeeded")
	}
}
