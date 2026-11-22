package logger

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
)

const readBufferBytes = 8 << 10

// 短日志直接借用缓冲，长日志按需合并，仍遵守恢复时的单行大小上限。
func readLogLine(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadSlice('\n')
	if err != bufio.ErrBufferFull {
		return line, err
	}
	var record []byte
	for {
		length := len(record) + len(line)
		if length > maxLineBytes || (err == bufio.ErrBufferFull && length == maxLineBytes) {
			return nil, bufio.ErrBufferFull
		}
		record = append(record, line...)
		if err != bufio.ErrBufferFull {
			return record, err
		}
		line, err = reader.ReadSlice('\n')
	}
}

// seekAfter 按字节二分定位 ID > after 的第一条记录，不建立常驻历史索引。
// 中点可能位于行内：只解析其后的完整行，再从安全的行边界精确定位。
func seekAfter(ctx context.Context, file snapshotFile, after uint64, reader *bufio.Reader) (int64, error) {
	if after < file.meta.firstID {
		return 0, nil
	}
	if after >= file.meta.lastID {
		return file.meta.size, nil
	}
	lo, hi := int64(0), file.meta.size
	for lo < hi {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		mid := lo + (hi-lo)/2
		reader.Reset(io.NewSectionReader(file.file, mid, file.meta.size-mid))
		offset := mid
		if mid > 0 {
			var previous [1]byte
			if _, err := file.file.ReadAt(previous[:], mid-1); err != nil {
				return 0, fmt.Errorf("read log snapshot: %w", err)
			}
			if previous[0] != '\n' {
				line, err := readLogLine(reader)
				if err != nil {
					return 0, fmt.Errorf("read log snapshot: %w", err)
				}
				offset += int64(len(line))
			}
		}
		line, err := readLogLine(reader)
		if errors.Is(err, io.EOF) && len(line) == 0 && offset == file.meta.size {
			hi = mid
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("read log snapshot: %w", err)
		}
		entry, err := parseEntry(line)
		if err != nil {
			return 0, fmt.Errorf("decode log snapshot: %w", err)
		}
		if entry.ID > after {
			hi = mid
		} else {
			lo = offset + int64(len(line))
		}
	}
	reader.Reset(io.NewSectionReader(file.file, lo, file.meta.size-lo))
	for offset := lo; ; {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		line, err := readLogLine(reader)
		if err != nil {
			return 0, fmt.Errorf("read log snapshot: %w", err)
		}
		entry, err := parseEntry(line)
		if err != nil {
			return 0, fmt.Errorf("decode log snapshot: %w", err)
		}
		if entry.ID > after {
			return offset, nil
		}
		offset += int64(len(line))
	}
}

type forwardReader struct {
	files  []snapshotFile
	index  int
	after  uint64
	lastID uint64
	reader *bufio.Reader
	offset int64
	opened bool
}

func (r *forwardReader) next(ctx context.Context) (Entry, error) {
	for {
		if err := ctx.Err(); err != nil {
			return Entry{}, err
		}
		if !r.opened {
			for r.index < len(r.files) && r.files[r.index].meta.lastID <= r.after {
				r.index++
			}
			if r.index == len(r.files) {
				return Entry{}, io.EOF
			}
			if r.reader == nil {
				r.reader = bufio.NewReaderSize(nil, readBufferBytes)
			}
			file := r.files[r.index]
			offset, err := seekAfter(ctx, file, r.after, r.reader)
			if err != nil {
				return Entry{}, err
			}
			r.reader.Reset(io.NewSectionReader(file.file, offset, file.meta.size-offset))
			r.offset, r.opened = offset, true
		}
		file := r.files[r.index]
		line, err := readLogLine(r.reader)
		if errors.Is(err, io.EOF) && len(line) == 0 {
			if r.offset != file.meta.size {
				return Entry{}, fmt.Errorf("read log snapshot: %w", io.ErrUnexpectedEOF)
			}
			r.index++
			r.opened = false
			continue
		}
		if err != nil {
			return Entry{}, fmt.Errorf("read log snapshot: %w", err)
		}
		entry, err := parseEntry(line)
		if err != nil {
			return Entry{}, fmt.Errorf("decode log snapshot: %w", err)
		}
		if entry.ID <= r.lastID || entry.ID < file.meta.firstID || entry.ID > file.meta.lastID {
			return Entry{}, errors.New("invalid log snapshot ID order")
		}
		r.offset += int64(len(line))
		r.lastID = entry.ID
		return entry, nil
	}
}

// reverseReader 以有界块缓冲倒序读取物理行；跨块的单条记录最多占 maxLineBytes。
// next 返回的切片只在下次 next/reset 前有效。
type reverseReader struct {
	file     io.ReaderAt
	position int64
	buffer   []byte
}

func (r *reverseReader) reset(file io.ReaderAt, end int64) {
	r.file, r.position = file, end
	if r.buffer == nil {
		r.buffer = make([]byte, 0, 2*maxLineBytes)
	}
	r.buffer = r.buffer[:0]
}

func (r *reverseReader) next(ctx context.Context) ([]byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(r.buffer) > 0 {
			if r.buffer[len(r.buffer)-1] != '\n' {
				return nil, io.ErrUnexpectedEOF
			}
			if index := bytes.LastIndexByte(r.buffer[:len(r.buffer)-1], '\n'); index >= 0 {
				line := r.buffer[index+1:]
				if len(line) > maxLineBytes {
					return nil, bufio.ErrBufferFull
				}
				r.buffer = r.buffer[:index+1]
				return line, nil
			}
			if len(r.buffer) > maxLineBytes {
				return nil, bufio.ErrBufferFull
			}
			if r.position == 0 {
				line := r.buffer
				r.buffer = r.buffer[:0]
				return line, nil
			}
		} else if r.position == 0 {
			return nil, io.EOF
		}
		count := int(min(r.position, int64(maxLineBytes)))
		oldLength := len(r.buffer)
		r.buffer = r.buffer[:oldLength+count]
		copy(r.buffer[count:], r.buffer[:oldLength])
		n, err := r.file.ReadAt(r.buffer[:count], r.position-int64(count))
		if errors.Is(err, io.EOF) {
			return nil, io.ErrUnexpectedEOF
		}
		if err != nil {
			return nil, err
		}
		if n != count {
			return nil, io.ErrUnexpectedEOF
		}
		r.position -= int64(count)
	}
}

// Replay 复用快照的顺序文件读取位置，分页之间最多暂存一条匹配的前瞻记录。
// 不可并发调用 Next；使用完毕或出错后调用方应关闭所属 Snapshot。
type Replay struct {
	snapshot *Snapshot
	reader   forwardReader
	matcher  Matcher
	limit    int
	cursor   uint64
	reset    string
	pending  Entry
	done     bool
	err      error
}

// NewReplay 创建只支持 after 游标的顺序分页读取器，复用调用方的快照。
func (s *Snapshot) NewReplay(options QueryOptions) (*Replay, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	if options.After == nil {
		return nil, errors.New("replay requires an after cursor")
	}
	after, reset := s.NormalizeCursor(*options.After)
	return &Replay{
		snapshot: s, reader: forwardReader{files: s.files, after: after, lastID: after},
		matcher: options.Filter.Compile(), limit: options.Limit, cursor: after, reset: reset,
	}, nil
}

// Next 返回下一页及已扫描游标；读取错误为终止状态，不能继续跳过失败页。
func (r *Replay) Next(ctx context.Context) (QueryResult, error) {
	if r.err != nil {
		return QueryResult{}, r.err
	}
	if err := ctx.Err(); err != nil {
		r.err = err
		return QueryResult{}, err
	}
	result := QueryResult{
		Items: make([]Entry, 0, r.limit), MaxLimit: MaxLimit, Retention: r.snapshot.Retention,
		Gap: r.reset != "", Reset: r.reset,
	}
	r.reset = ""
	if r.pending.ID != 0 {
		result.Items = append(result.Items, r.pending)
		r.cursor = r.pending.ID
		r.pending = Entry{}
	}
	for !r.done {
		entry, err := r.reader.next(ctx)
		if err == io.EOF {
			r.done = true
			r.cursor = r.snapshot.Retention.NewestID
			break
		}
		if err != nil {
			r.err = err
			return QueryResult{}, err
		}
		matched := r.matcher.Matches(entry)
		if matched && len(result.Items) == r.limit {
			r.pending = entry
			result.HasMore = true
			break
		}
		r.cursor = entry.ID
		if matched {
			result.Items = append(result.Items, entry)
		}
	}
	result.NextAfter = r.cursor
	return result, nil
}
