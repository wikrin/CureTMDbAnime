package main

import (
	"fmt"
	"net/http"
	"time"

	"curetmdbanime/internal/api"
	"curetmdbanime/internal/config"
	"curetmdbanime/internal/logger"
)

func init() {
	if err := config.LoadConfig(); err != nil {
		logger.Error("配置加载失败: %v", err)
		return
	}
}

func main() {
	defer func() {
		logger.Info("TMDB Cure Proxy 服务停止")
	}()

	router := api.SetupRouter()

	addr := fmt.Sprintf("%s:%d", config.AppSettings.HOST, config.AppSettings.PORT)
	server := &http.Server{
		Addr:           addr,
		Handler:        router,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	logger.Info("Server listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatal("listen: %v", err)
	}
}
