package main

import (
	"fmt"
	"net/http"
	"os"
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

	defer logger.Info("TMDB Cure Proxy 服务停止")

	router := api.SetupRouter()
	addr := fmt.Sprintf("%s:%d", config.AppSettings.Host, config.AppSettings.Port)
	server := &http.Server{
		Addr:           addr,
		Handler:        router,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	logger.Info("Server listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatal("listen: %v", err)
	}
}
