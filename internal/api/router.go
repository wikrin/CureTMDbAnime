package api

import (
	"curetmdbanime/internal/logger"

	"github.com/gin-gonic/gin"
)

// SetupRouter 初始化 Gin 引擎并注册所有路由
func SetupRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	router.Use(proxyMiddleware)

	// API 路由分组
	apiRouter := router.Group("/3/tv")
	{
		RegisterTVRoutes(apiRouter) // 注册 TV 路由
	}
	// 全局代理路由（捕获所有未匹配路由）
	router.NoRoute(ProxyHandler)

	return router
}

func proxyMiddleware(c *gin.Context) {
	logger.Info("收到请求: %s %s", c.Request.Method, c.Request.URL.Path)
	c.Next()
}
