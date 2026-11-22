package logger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type countedSnapshotReader struct {
	data      *bytes.Reader
	calls     atomic.Int64
	bytesRead atomic.Int64
}

func (r *countedSnapshotReader) ReadAt(data []byte, offset int64) (int, error) {
	n, err := r.data.ReadAt(data, offset)
	r.calls.Add(1)
	r.bytesRead.Add(int64(n))
	return n, err
}

func (*countedSnapshotReader) Close() error { return nil }

func readerEntries(count int) []Entry {
	entries := make([]Entry, count)
	for i := range entries {
		entries[i] = Entry{
			ID: uint64(i + 1), Timestamp: "2000-01-01T00:00:00Z", Level: "info",
			Message: fmt.Sprintf("fixture record %d for reader tests", i+1),
		}
	}
	return entries
}

func readerSnapshot(entries []Entry, split int) (*Snapshot, []*countedSnapshotReader) {
	snapshot := &Snapshot{Retention: Retention{
		OldestID: entries[0].ID, NewestID: entries[len(entries)-1].ID,
		MaxFiles: 2, MaxSizeBytes: mib,
	}}
	var readers []*countedSnapshotReader
	add := func(items []Entry) {
		var data []byte
		for _, entry := range items {
			data = append(data, formatEntry(entry)...)
		}
		reader := &countedSnapshotReader{data: bytes.NewReader(data)}
		readers = append(readers, reader)
		snapshot.files = append(snapshot.files, snapshotFile{
			file: reader,
			meta: logFile{size: int64(len(data)), firstID: items[0].ID, lastID: items[len(items)-1].ID},
		})
	}
	if split > 0 {
		add(entries[:split])
	}
	add(entries[split:])
	snapshot.Retention.FileCount = len(snapshot.files)
	return snapshot, readers
}

func matchingReaderEntries(entries []Entry, after, before *uint64, filter Filter) []Entry {
	var matched []Entry
	for _, entry := range entries {
		if (after == nil || entry.ID > *after) && (before == nil || entry.ID < *before) && filter.Matches(entry) {
			matched = append(matched, entry)
		}
	}
	return matched
}

func assertReaderQuery(t *testing.T, snapshot *Snapshot, entries []Entry, options QueryOptions) {
	t.Helper()
	page, err := snapshot.Query(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	var after *uint64
	if options.After != nil {
		normalized, _ := snapshot.NormalizeCursor(*options.After)
		after = &normalized
	}
	matched := matchingReaderEntries(entries, after, options.Before, options.Filter)
	hasMore := len(matched) > options.Limit
	want := matched
	if hasMore {
		if after != nil {
			want = matched[:options.Limit]
		} else {
			want = matched[len(matched)-options.Limit:]
		}
	}
	if !slices.Equal(page.Items, want) {
		t.Fatalf("items differ: after=%v before=%v filter=%+v limit=%d\ngot=%+v\nwant=%+v", options.After, options.Before, options.Filter, options.Limit, page.Items, want)
	}
	wantAfter := snapshot.Retention.NewestID
	if after != nil && hasMore {
		wantAfter = *after
		for _, entry := range entries {
			if entry.ID >= matched[options.Limit].ID {
				break
			}
			if entry.ID > *after {
				wantAfter = entry.ID
			}
		}
	} else if options.Before != nil {
		wantAfter = min(wantAfter, *options.Before-1)
	}
	if page.NextAfter != wantAfter {
		t.Fatalf("next_after=%d, want %d", page.NextAfter, wantAfter)
	}
	if after != nil {
		if page.HasMore != hasMore || page.HasOlder || page.NextBefore != 0 {
			t.Fatalf("unexpected forward pagination: %+v", page)
		}
	} else {
		var wantBefore uint64
		if len(want) > 0 {
			wantBefore = want[0].ID
		}
		if page.HasOlder != hasMore || page.NextBefore != wantBefore || page.HasMore != (options.Before != nil && hasMore) {
			t.Fatalf("unexpected backward pagination: %+v", page)
		}
	}
}

func TestReadersMatchLinearQueryWithVariableLinesAndIDGaps(t *testing.T) {
	entries := readerEntries(137)
	var id uint64 = 7
	for i := range entries {
		id += uint64(i%4 + 1)
		entries[i].ID = id
		length := (i * 997) % maxMessageBytes
		switch i % 3 {
		case 0:
			entries[i].Level = "error"
			entries[i].Message = strings.Repeat("\x00", length) + " TMDB 番剧"
		case 1:
			entries[i].Message = strings.Repeat("番", length/3) + "\nnext\t" + `literal\n`
		default:
			entries[i].Message = strings.Repeat("X", length) + " other"
		}
		entries[i].Message = sanitize(entries[i].Message)
	}
	snapshot, _ := readerSnapshot(entries, 61)
	defer snapshot.Close()
	boundaries := []uint64{1, entries[0].ID - 1, entries[0].ID, id, id + 1, id + 2, math.MaxUint64}
	for i := 0; i < len(entries); i += 11 {
		boundaries = append(boundaries, entries[i].ID, entries[i].ID+1)
	}
	for _, filter := range []Filter{{}, {Level: "ERROR"}, {Keyword: "tmdb 番剧"}, {Keyword: "no match"}} {
		for _, limit := range []int{1, 7, 200} {
			assertReaderQuery(t, snapshot, entries, QueryOptions{Limit: limit, Filter: filter})
			for _, boundary := range boundaries {
				assertReaderQuery(t, snapshot, entries, QueryOptions{After: &boundary, Limit: limit, Filter: filter})
				assertReaderQuery(t, snapshot, entries, QueryOptions{Before: &boundary, Limit: limit, Filter: filter})
			}
		}
	}
}

func TestReplayReadsEachSnapshotFileOnlyOnce(t *testing.T) {
	entries := readerEntries(6000)
	snapshot, readers := readerSnapshot(entries, 3000)
	defer snapshot.Close()
	after := uint64(0)
	replay, err := snapshot.NewReplay(QueryOptions{After: &after, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for {
		page, err := replay.Next(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.Items[0] != entries[count] {
			t.Fatalf("replay lost/duplicated a record on page %d: %+v", count, page)
		}
		count++
		if !page.HasMore {
			break
		}
	}
	if count != len(entries) {
		t.Fatalf("replayed %d entries, want %d", count, len(entries))
	}
	for _, reader := range readers {
		if reader.bytesRead.Load() != reader.data.Size() {
			t.Fatalf("file reread between pages: read=%d size=%d", reader.bytesRead.Load(), reader.data.Size())
		}
	}
	if page, err := replay.Next(t.Context()); err != nil || len(page.Items) != 0 || page.HasMore || page.NextAfter != 6000 {
		t.Fatalf("finished replay did not stay finished: %+v %v", page, err)
	}
}

func TestReplayAdvancesFilteredCursorAndKeepsSnapshotBoundary(t *testing.T) {
	entries := readerEntries(8)
	for i := range entries {
		if entries[i].ID%3 == 0 {
			entries[i].Level, entries[i].Message = "error", "KEEP 番剧"
		}
	}
	snapshot, readers := readerSnapshot(entries, 4)
	defer snapshot.Close()
	last := readers[1]
	data := make([]byte, last.data.Size())
	_, _ = last.data.ReadAt(data, 0)
	data = append(data, formatEntry(Entry{ID: 9, Timestamp: entries[0].Timestamp, Level: "error", Message: "KEEP live"})...)
	last.data = bytes.NewReader(data) // 快照之后追加的内容不能进入补发。
	after := uint64(0)
	replay, err := snapshot.NewReplay(QueryOptions{After: &after, Limit: 1, Filter: Filter{Level: "ERROR", Keyword: "keep"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		id, cursor uint64
		more       bool
	}{{3, 5, true}, {6, 8, false}} {
		page, err := replay.Next(t.Context())
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != want.id || page.NextAfter != want.cursor || page.HasMore != want.more {
			t.Fatalf("filtered replay: %+v %v", page, err)
		}
	}
	assertReaderQuery(t, snapshot, entries, QueryOptions{Limit: 1})
}

func TestLatestAndBeforeReadersStopAfterOneExtraMatch(t *testing.T) {
	entries := readerEntries(6000)
	for _, before := range []*uint64{nil, new(uint64)} {
		if before != nil {
			*before = 3001
		}
		snapshot, readers := readerSnapshot(entries, 3000)
		assertReaderQuery(t, snapshot, entries, QueryOptions{Before: before, Limit: 2})
		if readers[0].calls.Load()+readers[1].calls.Load() != 1 {
			t.Fatalf("small tail query scanned history: calls=%d/%d", readers[0].calls.Load(), readers[1].calls.Load())
		}
		snapshot.Close()
	}
}

func TestForwardCursorSeeksWithoutScanningOldPrefix(t *testing.T) {
	entries := readerEntries(6000)
	snapshot, readers := readerSnapshot(entries, 3000)
	defer snapshot.Close()
	after := uint64(5998)
	assertReaderQuery(t, snapshot, entries, QueryOptions{After: &after, Limit: 1})
	if readers[0].calls.Load() != 0 || readers[1].bytesRead.Load() >= readers[1].data.Size()/2 {
		t.Fatalf("cursor query rescanned old prefix: calls=%d read=%d size=%d", readers[0].calls.Load(), readers[1].bytesRead.Load(), readers[1].data.Size())
	}
}

func TestReaderLineBoundsAndIncompleteRecords(t *testing.T) {
	base := readerEntries(1)[0]
	prefixBytes := len(formatEntry(Entry{ID: base.ID, Timestamp: base.Timestamp, Level: base.Level}))
	for _, length := range []int{readBufferBytes + 1, maxLineBytes - prefixBytes, maxLineBytes - prefixBytes + 1} {
		entry := base
		entry.Message = strings.Repeat("x", length)
		snapshot, _ := readerSnapshot([]Entry{entry}, 0)
		after := uint64(0)
		for _, options := range []QueryOptions{{Limit: 1}, {After: &after, Limit: 1}} {
			page, err := snapshot.Query(t.Context(), options)
			if length > maxLineBytes-prefixBytes {
				if err == nil {
					t.Fatal("oversized physical record accepted")
				}
			} else if err != nil || len(page.Items) != 1 || page.Items[0] != entry {
				t.Fatalf("bounded long line did not round trip: length=%d err=%v", length, err)
			}
		}
		snapshot.Close()
	}
	for _, truncate := range []bool{false, true} {
		snapshot, readers := readerSnapshot([]Entry{base}, 0)
		data := formatEntry(base)
		readers[0].data = bytes.NewReader(data[:len(data)-1])
		if !truncate {
			snapshot.files[0].meta.size-- // 文件本身保存半条尾记录，而非快照之后截断。
		}
		after := uint64(0)
		for _, options := range []QueryOptions{{Limit: 1}, {After: &after, Limit: 1}} {
			if _, err := snapshot.Query(t.Context(), options); err == nil {
				t.Fatalf("incomplete/truncated record hidden as EOF, truncate=%v", truncate)
			}
		}
		snapshot.Close()
	}
}

func TestReplayValidationCancellationAndTerminalErrors(t *testing.T) {
	snapshot, _ := readerSnapshot(readerEntries(20), 10)
	defer snapshot.Close()
	after, before := uint64(0), uint64(10)
	for _, options := range []QueryOptions{
		{Limit: 1}, {Before: &before, Limit: 1}, {After: &after, Before: &before, Limit: 1},
		{After: &after, Limit: 0}, {After: &after, Limit: MaxLimit + 1},
	} {
		if _, err := snapshot.NewReplay(options); err == nil {
			t.Fatal("invalid replay options accepted")
		}
	}
	replay, err := snapshot.NewReplay(QueryOptions{After: &after, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := replay.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := replay.Next(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatal("failed replay resumed and could skip undelivered entries")
	}
}

func TestConcurrentQueriesOnOneSnapshot(t *testing.T) {
	entries := readerEntries(1000)
	snapshot, _ := readerSnapshot(entries, 500)
	defer snapshot.Close()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			boundary := uint64(100 + i*100)
			for j := 0; j < 10; j++ {
				assertReaderQuery(t, snapshot, entries, QueryOptions{After: &boundary, Limit: 7})
				assertReaderQuery(t, snapshot, entries, QueryOptions{Before: &boundary, Limit: 7})
			}
		}(i)
	}
	wg.Wait()
}

func FuzzReaderPagination(f *testing.F) {
	f.Add([]byte{0, 1, 2, 255, 8, 4, 63}, uint64(12), uint8(3))
	f.Add([]byte{255, 255, 255, 0, 255}, uint64(0), uint8(1))
	f.Fuzz(func(t *testing.T, seed []byte, cursor uint64, pageSize uint8) {
		if len(seed) == 0 {
			return
		}
		seed = seed[:min(len(seed), 40)]
		entries := readerEntries(len(seed))
		var id uint64
		for i, value := range seed {
			id += uint64(value%4 + 1)
			entries[i].ID = id
			entries[i].Message = sanitize(strings.Repeat("番\x00\n\\", int(value)*3))
		}
		snapshot, _ := readerSnapshot(entries, len(entries)/2)
		defer snapshot.Close()
		limit := int(pageSize%8) + 1
		after := cursor % (id + 3)
		before := after + 1
		assertReaderQuery(t, snapshot, entries, QueryOptions{After: &after, Limit: limit})
		assertReaderQuery(t, snapshot, entries, QueryOptions{Before: &before, Limit: limit})
		assertReaderQuery(t, snapshot, entries, QueryOptions{Limit: limit})
	})
}

var _ snapshotReader = (*countedSnapshotReader)(nil)
