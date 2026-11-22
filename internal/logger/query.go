package logger

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
)

// Retention 描述文件快照的保留范围，不暴露磁盘路径。
type Retention struct {
	OldestID     uint64 `json:"oldest_id"`
	NewestID     uint64 `json:"newest_id"`
	FileCount    int    `json:"file_count"`
	MaxFiles     int    `json:"max_files"`
	MaxSizeBytes int64  `json:"max_size_bytes"`
}

type snapshotReader interface {
	io.ReaderAt
	io.Closer
}

type snapshotFile struct {
	file snapshotReader
	meta logFile
}

// Snapshot 持有已打开的文件及固定长度，备份重命名/删除不改变正在读取的快照。
// 调用方必须 Close；不能在 Query 或 Replay 读取期间关闭快照。
type Snapshot struct {
	files     []snapshotFile
	Retention Retention
}

func (s *Store) snapshotLocked() (*Snapshot, error) {
	if s.closed {
		return nil, ErrClosed
	}
	if s.writeErr != nil {
		return nil, fmt.Errorf("persistent log unavailable: %w", s.writeErr)
	}
	for attempt := 0; attempt < 3; attempt++ {
		if s.currentFileLocked() == nil {
			return nil, errors.New("current log file unavailable")
		}
		snapshot := &Snapshot{Retention: s.retentionLocked()}
		retry := false
		for _, meta := range s.files {
			if meta.firstID == 0 {
				continue
			}
			file, err := os.Open(meta.path)
			if errors.Is(err, os.ErrNotExist) && filepath.Base(meta.path) != fileName {
				// 库的异步清理可能先于 open 删除旧备份，重新获取完整快照。
				retry = true
				break
			}
			if err != nil {
				snapshot.Close()
				return nil, fmt.Errorf("open log snapshot: %w", err)
			}
			snapshot.files = append(snapshot.files, snapshotFile{file: file, meta: meta})
		}
		if !retry {
			return snapshot, nil
		}
		snapshot.Close()
		if err := s.refreshFilesLocked(false); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("log retention changed while opening snapshot")
}

func (s *Store) Snapshot() (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *Snapshot) Close() {
	for _, file := range s.files {
		_ = file.file.Close()
	}
	s.files = nil
}

type QueryOptions struct {
	After  *uint64 // 正向读取 ID > after 的第一批匹配项。
	Before *uint64 // 向前翻页，读取 ID < before 的最近一批匹配项；与 After 互斥。
	Limit  int
	Filter
}

type QueryResult struct {
	Items      []Entry   `json:"items"`
	NextAfter  uint64    `json:"next_after"`
	HasMore    bool      `json:"has_more"`
	NextBefore uint64    `json:"next_before"` // 最新页/before 页的更早游标；after 页使用正向字段。
	HasOlder   bool      `json:"has_older"`   // 是否还有符合筛选条件的更早记录。
	MaxLimit   int       `json:"max_limit"`
	Retention  Retention `json:"retention"`
	Gap        bool      `json:"gap"`
	Reset      string    `json:"reset,omitempty"`
}

func (o QueryOptions) Validate() error {
	if o.After != nil && o.Before != nil {
		return errors.New("after and before are mutually exclusive")
	}
	if o.Before != nil && *o.Before == 0 {
		return errors.New("before must be a positive log ID")
	}
	if o.Limit < 1 || o.Limit > MaxLimit {
		return errors.New("invalid pagination")
	}
	if o.Level != "" && !validLevel(o.Level) {
		return errors.New("invalid log level")
	}
	if len(o.Keyword) > maxMessageBytes {
		return errors.New("keyword too long")
	}
	return nil
}

func (s *Snapshot) NormalizeCursor(after uint64) (uint64, string) {
	rangeInfo := s.Retention
	if after > rangeInfo.NewestID {
		if rangeInfo.OldestID > 0 {
			return rangeInfo.OldestID - 1, "cursor_ahead"
		}
		return rangeInfo.NewestID, "cursor_ahead"
	}
	if rangeInfo.OldestID > 0 && after < rangeInfo.OldestID-1 {
		return rangeInfo.OldestID - 1, "history_pruned"
	}
	if rangeInfo.OldestID == 0 && after < rangeInfo.NewestID {
		return rangeInfo.NewestID, "history_pruned"
	}
	return after, ""
}

func (s *Store) Query(ctx context.Context, options QueryOptions) (QueryResult, error) {
	if err := options.Validate(); err != nil {
		return QueryResult{}, err
	}
	snapshot, err := s.Snapshot()
	if err != nil {
		return QueryResult{}, err
	}
	defer snapshot.Close()
	return snapshot.Query(ctx, options)
}

func Query(ctx context.Context, options QueryOptions) (QueryResult, error) {
	store := activeStore.Load()
	if store == nil {
		return QueryResult{}, ErrNotInitialized
	}
	return store.Query(ctx, options)
}

func (s *Snapshot) Query(ctx context.Context, options QueryOptions) (QueryResult, error) {
	if err := options.Validate(); err != nil {
		return QueryResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return QueryResult{}, err
	}
	if options.After != nil {
		replay, err := s.NewReplay(options)
		if err != nil {
			return QueryResult{}, err
		}
		return replay.Next(ctx)
	}

	result := QueryResult{
		Items: make([]Entry, 0, options.Limit), MaxLimit: MaxLimit,
		Retention: s.Retention, NextAfter: s.Retention.NewestID,
	}
	if options.Before != nil {
		before := *options.Before
		if before > s.Retention.NewestID && before-s.Retention.NewestID > 1 {
			result.Reset = "cursor_ahead"
		}
		if (s.Retention.OldestID > 1 && before <= s.Retention.OldestID) ||
			(s.Retention.OldestID == 0 && s.Retention.NewestID > 0) {
			result.Reset = "history_pruned"
		}
		result.Gap = result.Reset != ""
		// 过旧的 before 不重置为最近一页，避免重复返回更新的记录。
		result.NextAfter = min(s.Retention.NewestID, before-1)
	}

	matcher := options.Filter.Compile()
	var reader reverseReader
	var seeker *bufio.Reader
	var previousID uint64
scan:
	for index := len(s.files) - 1; index >= 0; index-- {
		file := s.files[index]
		end := file.meta.size
		if options.Before != nil {
			if file.meta.firstID >= *options.Before {
				continue
			}
			if file.meta.lastID >= *options.Before {
				if seeker == nil {
					seeker = bufio.NewReaderSize(nil, readBufferBytes)
				}
				var err error
				end, err = seekAfter(ctx, file, *options.Before-1, seeker)
				if err != nil {
					return QueryResult{}, err
				}
			}
		}
		reader.reset(file.file, end)
		for {
			line, err := reader.next(ctx)
			if err == io.EOF {
				break
			}
			if err != nil {
				return QueryResult{}, fmt.Errorf("read log snapshot: %w", err)
			}
			entry, err := parseEntry(line)
			if err != nil {
				return QueryResult{}, fmt.Errorf("decode log snapshot: %w", err)
			}
			if (previousID != 0 && entry.ID >= previousID) || entry.ID < file.meta.firstID || entry.ID > file.meta.lastID {
				return QueryResult{}, errors.New("invalid log snapshot ID order")
			}
			previousID = entry.ID
			if !matcher.Matches(entry) {
				continue
			}
			if len(result.Items) == options.Limit {
				result.HasOlder = true
				break scan
			}
			result.Items = append(result.Items, entry)
		}
	}
	slices.Reverse(result.Items)
	if len(result.Items) > 0 {
		result.NextBefore = result.Items[0].ID
	}
	if options.Before != nil {
		result.HasMore = result.HasOlder
	}
	return result, ctx.Err()
}
