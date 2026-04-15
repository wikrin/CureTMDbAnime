package api

import (
	"curetmdbanime/internal/logger"

	"github.com/gin-gonic/gin"
)

// 初始化 Gin 引擎并注册所有路由
func SetupRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	router.Use(proxyMiddleware)

	// API 路由分组
	TmdbTVRouter := router.Group("/3/tv")
	{
		RegisterTVRoutes(TmdbTVRouter)
	}

	// 缓存路由分组
	cacheRouter := router.Group("/cache")
	{
		RegisterCacheRoutes(cacheRouter)
	}

	// 全局代理路由（捕获所有未匹配路由）
	router.NoRoute(ProxyHandler)

	return router
}

func proxyMiddleware(c *gin.Context) {
	logger.Debug("收到请求: %s %s", c.Request.Method, c.Request.URL.Path)
	c.Next()
}
