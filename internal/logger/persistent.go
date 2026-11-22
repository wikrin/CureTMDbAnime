package logger

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	DefaultLimit = 200
	MaxLimit     = 2000
	fileName     = "curetmdb.log"
	filePrefix   = "curetmdb-"
	fileSuffix   = ".log"
	maxLineBytes = 64 << 10
	mib          = 1 << 20
)

var (
	ErrNotInitialized = errors.New("persistent logger not initialized")
	ErrClosed         = errors.New("persistent logger closed")
	activeStore       atomic.Pointer[Store]
	lifecycleMu       sync.Mutex
)

type logFile struct {
	path            string
	size            int64
	firstID, lastID uint64
	info            os.FileInfo
	rotatedAt       time.Time
}

// Store 保留查询所需的文件元信息；文件写入和大小轮转由 Lumberjack 执行。
// 一个目录只允许一个进程写入，不在内存中保存历史日志。
type Store struct {
	mu            sync.Mutex
	dir           string
	maxBytes      int64
	maxFiles      int
	files         []logFile
	writer        io.WriteCloser
	sequence      uint64
	persistedID   uint64
	checkpointID  uint64
	closed        bool
	writeErr      error
	lastReport    time.Time
	subscribers   map[*Subscription]struct{}
	streamsClosed bool
}

func Initialize(dataDir string, maxSizeMiB, maxFiles int) error {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	if activeStore.Load() != nil {
		return errors.New("persistent logger already initialized")
	}
	if maxSizeMiB <= 0 || int64(maxSizeMiB) > math.MaxInt64/mib {
		return errors.New("invalid log file size")
	}
	store, err := OpenStore(filepath.Join(dataDir, "logs"), int64(maxSizeMiB)*mib, maxFiles)
	if err != nil {
		return err
	}
	activeStore.Store(store)
	return nil
}

func Close() error {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	if store := activeStore.Swap(nil); store != nil {
		return store.Close()
	}
	return nil
}

// OpenStore 的大小单位为字节，必须是整数 MiB，与 Lumberjack 的 MaxSize 一致。
func OpenStore(dir string, maxBytes int64, maxFiles int) (_ *Store, err error) {
	if maxBytes < mib || maxBytes%mib != 0 || uint64(maxBytes/mib) > uint64(^uint(0)>>1) || maxFiles <= 0 {
		return nil, errors.New("invalid log retention settings")
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	store := &Store{dir: dir, maxBytes: maxBytes, maxFiles: maxFiles}
	store.sequence, err = readCheckpoint(dir)
	if err != nil {
		return nil, err
	}
	store.checkpointID = store.sequence
	if err := store.refreshFilesLocked(true); err != nil {
		return nil, err
	}
	var previous uint64
	for _, file := range store.files {
		if file.firstID != 0 && file.firstID <= previous {
			return nil, errors.New("overlapping log file IDs")
		}
		previous = max(previous, file.lastID)
	}
	store.sequence = max(store.sequence, previous)
	store.persistedID = store.sequence
	// Lumberjack 的备份清理是异步的；先保存 ID，避免仅保留当前文件时丢失游标。
	if err := store.checkpointLocked(); err != nil {
		return nil, err
	}
	// 在启动 Lumberjack 的异步清理前按新大小/数量收敛，避免它抢先按
	// 备份总数删除合规文件。超大当前文件的最后 ID 已保存，可以直接淘汰。
	if err := store.pruneFilesLocked(); err != nil {
		return nil, err
	}
	rotator := &lumberjack.Logger{
		Filename:   filepath.Join(dir, fileName),
		MaxSize:    int(maxBytes / mib),
		MaxBackups: maxFiles - 1,
		MaxAge:     0,
		Compress:   false,
		LocalTime:  false,
	}
	store.writer = rotator
	defer func() {
		if err != nil {
			_ = rotator.Close()
		}
	}()
	if current := store.currentFileLocked(); current != nil {
		if err := os.Chmod(current.path, 0600); err != nil {
			return nil, err
		}
		if current.size >= maxBytes {
			if err := syncFile(current.path); err != nil {
				return nil, err
			}
			if err := rotator.Rotate(); err != nil {
				return nil, err
			}
		}
	}
	// 初始化时就打开文件，不能等第一个 HTTP 请求才发现权限/磁盘错误。
	if _, err := rotator.Write(nil); err != nil {
		return nil, err
	}
	if err := store.refreshFilesLocked(false); err != nil {
		return nil, err
	}
	if err := store.pruneFilesLocked(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) currentFileLocked() *logFile {
	for i := range s.files {
		if filepath.Base(s.files[i].path) == fileName {
			return &s.files[i]
		}
	}
	return nil
}

func (s *Store) Append(level, message string) (Entry, error) {
	if !validLevel(level) {
		return Entry{}, errors.New("invalid log level")
	}
	message = sanitize(message)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Entry{}, ErrClosed
	}
	if s.sequence == math.MaxUint64 {
		return Entry{}, errors.New("log IDs exhausted")
	}
	entry := Entry{ID: s.sequence + 1, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Level: strings.ToLower(level), Message: message}
	if s.writeErr == nil {
		s.writeErr = s.appendLocked(entry)
	}
	s.sequence = entry.ID
	s.publishLocked(entry)
	return entry, s.writeErr
}

func (s *Store) appendLocked(entry Entry) error {
	line := formatEntry(entry)
	if int64(len(line)) > s.maxBytes {
		return errors.New("log record exceeds file size limit")
	}
	current := s.currentFileLocked()
	if current == nil {
		return errors.New("current log file unavailable")
	}
	old := *current
	willRotate := old.size+int64(len(line)) > s.maxBytes
	if willRotate {
		// Lumberjack 没有 Sync 方法，使用独立 FD 在自动轮转前同步当前文件。
		if err := syncFile(old.path); err != nil {
			return err
		}
		if err := s.checkpointLocked(); err != nil {
			return err
		}
	}
	written, err := s.writer.Write(line) // 大小检查、关闭、重命名、新建由 Lumberjack 完成。
	if err == nil && written != len(line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		// 最佳努力回退半条写入；重启时也会修复当前文件的不完整尾记录。
		if info, statErr := os.Stat(old.path); statErr == nil {
			size := int64(0)
			if os.SameFile(info, old.info) {
				size = old.size
			}
			_ = os.Truncate(old.path, size)
		}
		return err
	}
	s.persistedID = entry.ID
	if willRotate {
		// 通过文件身份复用元信息，轮转只扫描目录，不重新读取完整历史。
		if err := s.refreshFilesLocked(false); err != nil {
			return err
		}
		return s.pruneFilesLocked()
	}
	current.size += int64(written)
	if current.firstID == 0 {
		current.firstID = entry.ID
	}
	current.lastID = entry.ID
	return nil
}

func syncFile(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

func (s *Store) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	return errors.Join(s.writeErr, syncFile(filepath.Join(s.dir, fileName)), s.checkpointLocked())
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.stopStreamsLocked()
	return errors.Join(s.writeErr, syncFile(filepath.Join(s.dir, fileName)), s.checkpointLocked(), s.writer.Close())
}

func (s *Store) report(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.lastReport) >= time.Minute {
		fmt.Fprintf(os.Stderr, "[LOGGER] 持久化日志失败（历史查询不可用，请修复存储并重启）: %s\n", sanitize(err.Error()))
		s.lastReport = time.Now()
	}
}
