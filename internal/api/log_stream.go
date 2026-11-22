package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"curetmdbanime/internal/logger"
	"github.com/gin-gonic/gin"
)

const (
	streamHeartbeat    = 15 * time.Second
	streamWriteTimeout = 10 * time.Second
)

func parseLogQuery(c *gin.Context) (logger.QueryOptions, error) {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return logger.QueryOptions{}, errors.New("invalid query parameters")
	}
	options := logger.QueryOptions{Limit: logger.DefaultLimit, Filter: logger.Filter{Level: query.Get("level"), Keyword: query.Get("keyword")}}
	if values, exists := query["after"]; exists {
		if len(values) != 1 {
			return options, errors.New("invalid pagination")
		}
		after, err := strconv.ParseUint(values[0], 10, 64)
		if err != nil {
			return options, errors.New("invalid pagination")
		}
		options.After = &after
	}
	if values, exists := query["before"]; exists {
		if len(values) != 1 {
			return options, errors.New("invalid pagination")
		}
		before, err := strconv.ParseUint(values[0], 10, 64)
		if err != nil {
			return options, errors.New("invalid pagination")
		}
		options.Before = &before
	}
	if values, exists := query["limit"]; exists {
		if len(values) != 1 {
			return options, errors.New("invalid pagination")
		}
		limit, err := strconv.Atoi(values[0])
		if err != nil {
			return options, errors.New("invalid pagination")
		}
		options.Limit = limit
	}
	return options, options.Validate()
}

func logStream(c *gin.Context) {
	serveLogStream(c, streamHeartbeat, streamWriteTimeout)
}

func serveLogStream(c *gin.Context, heartbeat, writeTimeout time.Duration) {
	options, err := parseLogQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	if options.Before != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "before is only supported by log queries"})
		return
	}
	if options.After == nil && c.GetHeader("Last-Event-ID") != "" {
		after, err := strconv.ParseUint(c.GetHeader("Last-Event-ID"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "invalid Last-Event-ID"})
			return
		}
		options.After = &after
	}
	subscription, snapshot, err := logger.Subscribe(options.After != nil)
	if err != nil {
		status, message := http.StatusServiceUnavailable, "log stream unavailable"
		if errors.Is(err, logger.ErrStreamLimit) {
			status, message = http.StatusTooManyRequests, "too many log streams"
		}
		c.JSON(status, gin.H{"message": message})
		return
	}
	defer subscription.Close()
	defer snapshot.Close()
	var replay *logger.Replay
	if options.After != nil {
		replay, err = snapshot.NewReplay(options)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"message": "persistent log replay unavailable"})
			return
		}
	}
	matcher := options.Filter.Compile()

	// Gin 的 Flush 不返回底层错误，展开其 writer 后才能使用 FlushError。
	writer := http.ResponseWriter(c.Writer)
	if unwrapper, ok := writer.(interface{ Unwrap() http.ResponseWriter }); ok {
		writer = unwrapper.Unwrap()
	}
	controller := http.NewResponseController(writer)
	if err := controller.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		c.JSON(http.StatusNotImplemented, gin.H{"message": "streaming write deadlines unsupported"})
		return
	}
	// 在空闲心跳间隔内清除服务器的全局 WriteTimeout，仅约束单次流写入。
	defer controller.SetWriteDeadline(time.Time{})
	streamCtx, cancel := context.WithCancel(c.Request.Context())
	stopSubscription := context.AfterFunc(subscription.Context(), cancel)
	interrupted := make(chan struct{})
	stopWrite := context.AfterFunc(streamCtx, func() {
		_ = controller.SetWriteDeadline(time.Now())
		close(interrupted)
	})
	defer func() {
		stopSubscription()
		cancel()
		if !stopWrite() {
			<-interrupted
		}
	}()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-store")
	c.Header("X-Accel-Buffering", "no")
	write := func(payload string) error {
		if err := streamCtx.Err(); err != nil {
			return err
		}
		if err := controller.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
			return err
		}
		if _, err := c.Writer.WriteString(payload); err != nil {
			return err
		}
		if err := controller.Flush(); err != nil {
			return err
		}
		return controller.SetWriteDeadline(time.Time{})
	}
	event := func(name string, id uint64, data any) error {
		encoded, err := json.Marshal(data)
		if err != nil {
			return err
		}
		return write(fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", id, name, encoded))
	}
	cursor := snapshot.Retention.NewestID
	if options.After != nil {
		var reset string
		cursor, reset = snapshot.NormalizeCursor(*options.After)
		if reset != "" {
			if err := event("reset", cursor, gin.H{"reason": reset, "retention": snapshot.Retention}); err != nil {
				return
			}
		}
	}
	// 立即 flush 建立连接，不等到第一条新日志才发送 HTTP 头。
	if err := event("cursor", cursor, gin.H{"after": cursor}); err != nil {
		return
	}
	if replay != nil {
		for {
			page, err := replay.Next(streamCtx)
			if err != nil {
				if streamCtx.Err() == nil {
					_ = event("error", cursor, gin.H{"message": "persistent log replay unavailable"})
				}
				return
			}
			for _, entry := range page.Items {
				if err := event("log", entry.ID, entry); err != nil {
					return
				}
				cursor = entry.ID
			}
			cursor = page.NextAfter
			if err := event("cursor", cursor, gin.H{"after": cursor}); err != nil {
				return
			}
			if !page.HasMore {
				break
			}
		}
	}
	// 补发结束后立即释放文件描述符，不让长连接无限保留已轮转的旧文件。
	snapshot.Close()
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-streamCtx.Done():
			return
		default:
		}
		select {
		case <-streamCtx.Done():
			return
		case entry := <-subscription.Entries():
			if entry.ID <= cursor {
				continue
			}
			cursor = entry.ID
			// 使用与查询相同的匹配规则；被过滤记录的进度在心跳时合并发送。
			if matcher.Matches(entry) {
				if err := event("log", entry.ID, entry); err != nil {
					return
				}
			}
		case <-ticker.C:
			if err := write(": heartbeat\n\n"); err != nil {
				return
			}
			if err := event("cursor", cursor, gin.H{"after": cursor}); err != nil {
				return
			}
		}
	}
}
