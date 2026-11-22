package logger

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	backupTimeFormat = "2006-01-02T15-04-05.000"
	checkpointName   = "curetmdb.cursor"
)

// 只识别 Lumberjack 的当前文件及未压缩备份
func logFileTime(name string) (time.Time, bool) {
	if name == fileName {
		return time.Time{}, true
	}
	if len(name) != len(filePrefix)+len(backupTimeFormat)+len(fileSuffix) || !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, fileSuffix) {
		return time.Time{}, false
	}
	timestamp, err := time.Parse(backupTimeFormat, name[len(filePrefix):len(name)-len(fileSuffix)])
	return timestamp, err == nil
}

// 正常轮转复用同一文件的 ID/大小元信息，只解析新文件，不重扫全部历史日志。
func (s *Store) refreshFilesLocked(repairTail bool) error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("read log directory: %w", err)
	}
	files := make([]logFile, 0, len(s.files)+1)
	for _, entry := range entries {
		rotatedAt, owned := logFileTime(entry.Name())
		if !owned {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) && entry.Name() != fileName {
			continue // Lumberjack 的后台清理可能已经删除了过期备份。
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("log file is not a regular file: %s", entry.Name())
		}
		meta := logFile{path: filepath.Join(s.dir, entry.Name()), info: info, rotatedAt: rotatedAt}
		known := false
		for _, previous := range s.files {
			if previous.info != nil && os.SameFile(info, previous.info) && previous.size == info.Size() {
				meta.size, meta.firstID, meta.lastID = previous.size, previous.firstID, previous.lastID
				known = true
				break
			}
		}
		if !known {
			if err := recoverFile(&meta, repairTail && entry.Name() == fileName); err != nil {
				if errors.Is(err, os.ErrNotExist) && entry.Name() != fileName {
					continue
				}
				return fmt.Errorf("recover %s: %w", entry.Name(), err)
			}
		}
		files = append(files, meta)
	}
	sort.Slice(files, func(i, j int) bool {
		if filepath.Base(files[i].path) == fileName {
			return false
		}
		if filepath.Base(files[j].path) == fileName {
			return true
		}
		if files[i].firstID == files[j].firstID {
			return files[i].rotatedAt.Before(files[j].rotatedAt)
		}
		return files[i].firstID < files[j].firstID
	})
	s.files = files
	return nil
}

func recoverFile(meta *logFile, repairTail bool) error {
	flags := os.O_RDONLY
	if repairTail {
		flags = os.O_RDWR
	}
	file, err := os.OpenFile(meta.path, flags, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, maxLineBytes)
	var offset int64
	for {
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, io.EOF) {
			if len(line) > 0 {
				if !repairTail {
					return errors.New("incomplete record in rotated log file")
				}
				if err := file.Truncate(offset); err != nil {
					return err
				}
				if err := file.Sync(); err != nil {
					return err
				}
			}
			meta.size = offset
			return nil
		}
		if err != nil {
			return fmt.Errorf("read log record: %w", err)
		}
		entry, err := parseEntry(line)
		if err != nil {
			return fmt.Errorf("invalid log record: %w", err)
		}
		if entry.ID <= meta.lastID {
			return errors.New("invalid log record ID")
		}
		if meta.firstID == 0 {
			meta.firstID = entry.ID
		}
		meta.lastID = entry.ID
		offset += int64(len(line))
	}
}

// 补充同步收敛：库的 MaxBackups 仅计算备份且异步清理，0 表示无限保留。
// 这里确保 Append 返回时总文件数（含当前文件）已达标，也处理 maxFiles=1 和配置缩小。
func (s *Store) pruneFilesLocked() error {
	removed := make(map[string]bool)
	remove := func(meta logFile) error {
		if err := s.checkpointLocked(); err != nil {
			return err
		}
		if err := os.Remove(meta.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("prune log file: %w", err)
		}
		removed[meta.path] = true
		return nil
	}

	// 先淘汰超大文件，再计算合规备份的数量，避免多删较旧的合规文件。
	// 超大的当前文件只会在启动恢复时出现，此时 Lumberjack 尚未打开文件。
	var backups []logFile
	for _, meta := range s.files {
		if meta.size > s.maxBytes {
			if err := remove(meta); err != nil {
				return err
			}
		} else if filepath.Base(meta.path) != fileName {
			backups = append(backups, meta)
		}
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].rotatedAt.Before(backups[j].rotatedAt) })
	excess := max(0, len(backups)-(s.maxFiles-1))
	for _, meta := range backups[:excess] {
		if err := remove(meta); err != nil {
			return err
		}
	}
	kept := s.files[:0]
	for _, meta := range s.files {
		if !removed[meta.path] {
			kept = append(kept, meta)
		}
	}
	s.files = kept
	return nil
}

func readCheckpoint(dir string) (uint64, error) {
	path := filepath.Join(dir, checkpointName)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() || info.Size() > 21 {
		return 0, errors.New("invalid log cursor checkpoint")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, errors.New("invalid log cursor checkpoint")
	}
	return id, nil
}

// 只在轮转/清理/关闭时保存 ID，不增加每条日志的同步开销；不保存日志正文。
func (s *Store) checkpointLocked() error {
	if s.persistedID <= s.checkpointID {
		return nil
	}
	file, err := os.CreateTemp(s.dir, ".curetmdb-cursor-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.WriteString(strconv.FormatUint(s.persistedID, 10) + "\n")
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil {
		return err
	}
	if err := os.Rename(file.Name(), filepath.Join(s.dir, checkpointName)); err != nil {
		return err
	}
	s.checkpointID = s.persistedID
	return nil
}
