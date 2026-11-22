package logger

import (
	"context"
	"errors"
)

const (
	MaxStreams        = 16
	SubscriptionQueue = 256
)

var ErrStreamLimit = errors.New("too many log streams")

// Subscription 只有用于传输的有界队列，不提供历史日志查询。
type Subscription struct {
	store   *Store
	entries chan Entry
	ctx     context.Context
	cancel  context.CancelFunc
}

func (s *Subscription) Entries() <-chan Entry    { return s.entries }
func (s *Subscription) Done() <-chan struct{}    { return s.ctx.Done() }
func (s *Subscription) Context() context.Context { return s.ctx }

func (s *Subscription) Close() {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	delete(s.store.subscribers, s)
	s.cancel()
}

// Subscribe 原子地捕获补发边界并注册订阅。队列中只会出现快照之后的记录。
// replay=false 不读取历史文件，即使磁盘故障仍可接收后续实时日志。
func (s *Store) Subscribe(replay bool) (*Subscription, *Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.streamsClosed {
		return nil, nil, ErrClosed
	}
	if len(s.subscribers) >= MaxStreams {
		return nil, nil, ErrStreamLimit
	}
	snapshot := &Snapshot{Retention: s.retentionLocked()}
	if replay {
		var err error
		snapshot, err = s.snapshotLocked()
		if err != nil {
			return nil, nil, err
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	subscription := &Subscription{store: s, entries: make(chan Entry, SubscriptionQueue), ctx: ctx, cancel: cancel}
	if s.subscribers == nil {
		s.subscribers = make(map[*Subscription]struct{})
	}
	s.subscribers[subscription] = struct{}{}
	return subscription, snapshot, nil
}

func Subscribe(replay bool) (*Subscription, *Snapshot, error) {
	if store := activeStore.Load(); store != nil {
		return store.Subscribe(replay)
	}
	return nil, nil, ErrNotInitialized
}

// 调用时持有 Store.mu；不能让慢消费者阻塞文件写入。
func (s *Store) publishLocked(entry Entry) {
	for subscription := range s.subscribers {
		select {
		case subscription.entries <- entry:
		default:
			delete(s.subscribers, subscription)
			subscription.cancel()
		}
	}
}

func (s *Store) stopStreamsLocked() {
	s.streamsClosed = true
	for subscription := range s.subscribers {
		delete(s.subscribers, subscription)
		subscription.cancel()
	}
}

// StopStreams 必须在 HTTP Shutdown 之前调用，否则长连接会阻止优雅退出。
func StopStreams() {
	if store := activeStore.Load(); store != nil {
		store.mu.Lock()
		defer store.mu.Unlock()
		store.stopStreamsLocked()
	}
}

func (s *Store) retentionLocked() Retention {
	retention := Retention{NewestID: s.sequence, FileCount: len(s.files), MaxFiles: s.maxFiles, MaxSizeBytes: s.maxBytes}
	for _, file := range s.files {
		if file.firstID != 0 {
			retention.OldestID = file.firstID
			break
		}
	}
	return retention
}

// Status 不暴露配置凭据或文件路径，只返回日志存储与连接状态。
type StorageStatus struct {
	Available   bool `json:"available"`
	StreamCount int  `json:"stream_count"`
	MaxStreams  int  `json:"max_streams"`
	Retention
}

func Status() StorageStatus {
	status := StorageStatus{MaxStreams: MaxStreams}
	if store := activeStore.Load(); store != nil {
		store.mu.Lock()
		defer store.mu.Unlock()
		status.Available = !store.closed && store.writeErr == nil
		status.StreamCount = len(store.subscribers)
		status.Retention = store.retentionLocked()
	}
	return status
}
