package logger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gopkg.in/natefinch/lumberjack.v2"
)

func openTestStore(t *testing.T, maxBytes int64, maxFiles int) *Store {
	t.Helper()
	store, err := OpenStore(t.TempDir(), maxBytes, maxFiles)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func injectWriterFailure(t *testing.T, store *Store) {
	t.Helper()
	if err := store.writer.Close(); err != nil {
		t.Fatal(err)
	}
	store.writer = failingLogWriter{}
}

type failingLogWriter struct{}

func (failingLogWriter) Write([]byte) (int, error) { return 0, errors.New("injected disk failure") }
func (failingLogWriter) Close() error              { return nil }

func appendTest(t *testing.T, store *Store, level, message string) Entry {
	t.Helper()
	entry, err := store.Append(level, message)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func queryTest(t *testing.T, store *Store, options QueryOptions) QueryResult {
	t.Helper()
	result, err := store.Query(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestLumberjackConfiguration(t *testing.T) {
	for _, maxFiles := range []int{1, 5} {
		store := openTestStore(t, 2*mib, maxFiles)
		rotator, ok := store.writer.(*lumberjack.Logger)
		if !ok || rotator.Filename != filepath.Join(store.dir, fileName) || rotator.MaxSize != 2 || rotator.MaxBackups != maxFiles-1 || rotator.MaxAge != 0 || rotator.Compress || rotator.LocalTime {
			t.Fatalf("unexpected Lumberjack configuration: %+v", store.writer)
		}
	}
}

func TestSingleFileEmptyRestartPreservesCursor(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "curetmdb-2000-01-01T00-00-00.000.log")
	entry := Entry{ID: 99, Timestamp: "2000-01-01T00:00:00Z", Level: "info", Message: "last retained ID"}
	if err := os.WriteFile(backup, formatEntry(entry), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dir, mib, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("single-file mode kept a backup")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(dir, mib, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if entry := appendTest(t, reopened, "info", "after empty restart"); entry.ID != 100 {
		t.Fatal(entry)
	}
}

func TestOldBackupHasNoAgeExpiry(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "curetmdb-2000-01-01T00-00-00.000.log")
	entry := Entry{ID: 1, Timestamp: "2000-01-01T00:00:00Z", Level: "info", Message: "not expired by time"}
	if err := os.WriteFile(backup, formatEntry(entry), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(dir, mib, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	appendTest(t, store, "info", "new record")
	after := uint64(0)
	page := queryTest(t, store, QueryOptions{After: &after, Limit: 10})
	if len(page.Items) != 2 || page.Items[0] != entry {
		t.Fatal(page)
	}
}

func TestCheckpointFailurePreventsSilentCursorReuse(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, checkpointName), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(dir, mib, 1); err == nil {
		t.Fatal("invalid checkpoint ignored")
	}
	store := openTestStore(t, mib, 1)
	appendTest(t, store, "info", "before checkpoint error")
	if err := os.Mkdir(filepath.Join(store.dir, checkpointName), 0700); err != nil {
		t.Fatal(err)
	}
	if err := store.Sync(); err == nil {
		t.Fatal("failed checkpoint hidden")
	}
}

func TestPersistentHistoryBeyondOldCapacityAndRestart(t *testing.T) {
	store := openTestStore(t, 1<<20, 3)
	for i := 0; i < 2010; i++ {
		appendTest(t, store, "info", fmt.Sprintf("record %d api_key=secret", i))
	}
	after := uint64(0)
	page := queryTest(t, store, QueryOptions{After: &after, Limit: 10})
	if len(page.Items) != 10 || page.Items[0].ID != 1 || page.NextAfter != 10 || !page.HasMore {
		t.Fatalf("first page: %+v", page)
	}
	if strings.Contains(page.Items[0].Message, "secret") {
		t.Fatal("persisted credential")
	}
	tail := queryTest(t, store, QueryOptions{Limit: 3})
	if len(tail.Items) != 3 || tail.Items[0].ID != 2008 || tail.NextAfter != 2010 || tail.HasMore {
		t.Fatalf("tail: %+v", tail)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(store.dir, 1<<20, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := appendTest(t, reopened, "error", "after restart").ID; got != 2011 {
		t.Fatal(got)
	}
	if got := queryTest(t, reopened, QueryOptions{After: &after, Limit: 1}); got.Items[0].ID != 1 {
		t.Fatal(got)
	}
}

func TestRotationAndSnapshot(t *testing.T) {
	for _, maxFiles := range []int{1, 3} {
		t.Run(fmt.Sprint(maxFiles), func(t *testing.T) {
			store := openTestStore(t, mib, maxFiles)
			appendTest(t, store, "info", "original")
			snapshot, err := store.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()
			for i := 0; i < 1024; i++ {
				appendTest(t, store, "info", strings.Repeat("x", maxMessageBytes))
			}
			entries, err := os.ReadDir(store.dir)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, entry := range entries {
				if _, owned := logFileTime(entry.Name()); !owned {
					continue
				}
				count++
				info, _ := entry.Info()
				if info.Size() > mib || info.Mode().Perm() != 0600 {
					t.Fatalf("file %s size=%d mode=%v", entry.Name(), info.Size(), info.Mode())
				}
			}
			if count > maxFiles {
				t.Fatal(count)
			}
			old, err := snapshot.Query(context.Background(), QueryOptions{Limit: 5})
			if err != nil || len(old.Items) != 1 || old.Items[0].Message != "original" {
				t.Fatalf("snapshot changed: %+v %v", old, err)
			}
			after := uint64(1)
			current := queryTest(t, store, QueryOptions{After: &after, Limit: 20})
			if !current.Gap || current.Reset != "history_pruned" || current.Items[0].ID <= 1 {
				t.Fatalf("missing rotation gap: %+v", current)
			}
		})
	}
}

func TestRecoveryTruncatedTailAndRetentionChange(t *testing.T) {
	store := openTestStore(t, 2*mib, 4)
	for i := 0; i < 530; i++ {
		appendTest(t, store, "info", strings.Repeat("x", maxMessageBytes))
	}
	lastPath := store.files[len(store.files)-1].path
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(lastPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString(`2026-07-05T10:20:30Z [INFO] [id=531] incomplete`)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(store.dir, "notes.txt")
	if err := os.WriteFile(unrelated, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(store.dir, mib, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if id := appendTest(t, reopened, "info", "recovered").ID; id != 531 {
		t.Fatal(id)
	}
	files, _ := os.ReadDir(store.dir)
	data, err := os.ReadFile(unrelated)
	if err != nil || string(data) != "keep" {
		t.Fatal(err)
	}
	count := 0
	for _, file := range files {
		if _, ok := logFileTime(file.Name()); ok {
			count++
			info, _ := file.Info()
			if info.Size() > mib {
				t.Fatal(info.Size())
			}
		}
	}
	if count != 1 {
		t.Fatal(count)
	}
}

func TestFilteredPaginationAdvancesPastNonmatches(t *testing.T) {
	store := openTestStore(t, 1<<20, 2)
	appendTest(t, store, "error", "TMDB 123 番剧")
	appendTest(t, store, "info", "TMDB 123 番剧")
	appendTest(t, store, "error", "other")
	appendTest(t, store, "error", "tmdb 123 番剧")
	appendTest(t, store, "info", "last")
	after := uint64(0)
	options := QueryOptions{After: &after, Limit: 1, Filter: Filter{Level: "ERROR", Keyword: "TmDb 123"}}
	page := queryTest(t, store, options)
	if len(page.Items) != 1 || page.Items[0].ID != 1 || page.NextAfter != 3 || !page.HasMore {
		t.Fatal(page)
	}
	after = page.NextAfter
	page = queryTest(t, store, options)
	if page.Items[0].ID != 4 || page.NextAfter != 5 || page.HasMore {
		t.Fatal(page)
	}
	after = 0
	options.Keyword = "no match"
	page = queryTest(t, store, options)
	if len(page.Items) != 0 || page.NextAfter != 5 || page.HasMore {
		t.Fatal(page)
	}
	after = 999
	options.Keyword = ""
	page = queryTest(t, store, options)
	if page.Reset != "cursor_ahead" || !page.Gap || page.Items[0].ID != 1 {
		t.Fatal(page)
	}
}

func TestSnapshotRefreshAfterBackupCleanup(t *testing.T) {
	store := openTestStore(t, mib, 2)
	for i := 0; i < 260; i++ {
		appendTest(t, store, "info", strings.Repeat("x", maxMessageBytes))
	}
	pinned, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	var backup string
	for _, file := range store.files {
		if filepath.Base(file.path) != fileName {
			backup = file.path
		}
	}
	if backup == "" {
		t.Fatal("no Lumberjack backup created")
	}
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	after := uint64(0)
	current := queryTest(t, store, QueryOptions{After: &after, Limit: 1})
	if !current.Gap || len(current.Items) != 1 || current.Items[0].ID <= 1 {
		t.Fatalf("backup cleanup not reflected: %+v", current)
	}
	old, err := pinned.Query(context.Background(), QueryOptions{After: &after, Limit: 1})
	if err != nil || len(old.Items) != 1 || old.Items[0].ID != 1 {
		t.Fatalf("cleanup broke an open snapshot: %+v %v", old, err)
	}
}

func TestConcurrentAppendAndQuery(t *testing.T) {
	store := openTestStore(t, mib, 5)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 40; j++ {
				if _, err := store.Append("info", strings.Repeat("c", maxMessageBytes)); err != nil {
					t.Error(err)
				}
				if j%10 != 0 {
					continue
				}
				result, err := store.Query(context.Background(), QueryOptions{Limit: 20})
				if err != nil {
					t.Error(err)
				}
				for k := 1; k < len(result.Items); k++ {
					if result.Items[k].ID <= result.Items[k-1].ID {
						t.Error("unordered IDs")
					}
				}
			}
		}()
	}
	wg.Wait()
	if tail := queryTest(t, store, QueryOptions{Limit: 1}); tail.NextAfter != 320 {
		t.Fatal(tail)
	}
}

func TestPersistenceErrorsNotEmptyResults(t *testing.T) {
	store := openTestStore(t, mib, 2)
	appendTest(t, store, "info", "before fault")
	injectWriterFailure(t, store)
	if _, err := store.Append("info", "disk failed"); err == nil {
		t.Fatal("expected write error")
	}
	if _, err := store.Query(context.Background(), QueryOptions{Limit: 10}); err == nil {
		t.Fatal("write failure hidden as an empty result")
	}
	if _, err := OpenStore(filepath.Join(t.TempDir(), "missing", "..", "file"), 0, 2); err == nil {
		t.Fatal("invalid settings accepted")
	}
}

func TestEncodedSizeAndEmptyFileRecovery(t *testing.T) {
	store := openTestStore(t, 1<<20, 3)
	// 转义后正文超过 1 MiB，但原始正文不到 1 MiB，验证库按文件字节数轮转。
	message := strings.Repeat("\x00", maxMessageBytes)
	for i := 0; i < 64; i++ {
		appendTest(t, store, "info", message)
	}
	if len(store.files) != 2 {
		t.Fatal("rotation used raw message length rather than encoded size")
	}
	for _, file := range store.files {
		if file.size > store.maxBytes {
			t.Fatal("file exceeded limit")
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, checkpointName), []byte("99\n"), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := OpenStore(dir, mib, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	after := uint64(0)
	page := queryTest(t, recovered, QueryOptions{After: &after, Limit: 1})
	if len(page.Items) != 0 || page.NextAfter != 99 || !page.Gap {
		t.Fatal(page)
	}
	if entry := appendTest(t, recovered, "info", "after interrupted rotation"); entry.ID != 100 {
		t.Fatal(entry)
	}
}

func TestFileAndSnapshotFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(path, mib, 1); err == nil {
		t.Fatal("file accepted as log directory")
	}
	dir := t.TempDir()
	managed := filepath.Join(dir, fileName)
	if err := os.WriteFile(managed, []byte("invalid log prefix\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(dir, mib, 1); err == nil {
		t.Fatal("corrupt complete record ignored")
	}
	store := openTestStore(t, mib, 1)
	appendTest(t, store, "info", "before delete")
	if err := os.Remove(store.files[0].path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Query(context.Background(), QueryOptions{Limit: 1}); err == nil {
		t.Fatal("missing file hidden as an empty result")
	}
}

func TestManagedSymlinkIsNotFollowed(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "keep")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(dir, fileName)
	if err := os.Symlink(target, managed); err != nil {
		t.Skip(err)
	}
	if _, err := OpenStore(dir, mib, 1); err == nil {
		t.Fatal("managed symlink accepted")
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "keep" {
		t.Fatal("symlink target changed")
	}
}

func TestQueryCancellationAndValidation(t *testing.T) {
	store := openTestStore(t, mib, 2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Query(ctx, QueryOptions{Limit: 10}); err == nil {
		t.Fatal("cancelled query succeeded")
	}
	for _, options := range []QueryOptions{{Limit: 0}, {Limit: 2001}, {Limit: 1, Filter: Filter{Level: "unknown"}}, {Limit: 1, Filter: Filter{Keyword: strings.Repeat("x", 4097)}}} {
		if _, err := store.Query(context.Background(), options); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
}
