package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"curetmdbanime/internal/api"
	"curetmdbanime/internal/cli"
	"curetmdbanime/internal/config"
	"curetmdbanime/internal/logger"
)

func main() {
	cliResult, handled, err := cli.Execute(os.Args[1:], config.Version)
	if err != nil {
		logger.Fatal("CLI 参数解析失败: %v", err)
	}
	if handled {
		return
	}

	if err := config.LoadConfig(cliResult.ConfigFlags); err != nil {
		logger.Fatal("配置加载失败: %v", err)
	}

	// 监听系统退出信号，收到中断时优雅关闭
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	defer logger.Info("TMDB Cure Proxy 服务停止")

	router := api.SetupRouter()
	addr := fmt.Sprintf("%s:%d", config.AppSettings.Host, config.AppSettings.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: router,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	go func() {
		// 等待退出信号后优雅关闭
		<-ctx.Done()
		logger.Info("正在优雅关闭服务...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("优雅关闭失败: %v", err)
		}
	}()

	logger.Info("Server listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal("listen: %v", err)
	}
}
