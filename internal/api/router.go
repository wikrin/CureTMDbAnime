package api

import (
	"fmt"
	"net/http"
	"path"
	"strings"

	"curetmdbanime/internal/logger"
	"github.com/gin-gonic/gin"
)

// 初始化 Gin 引擎并注册所有路由
func SetupRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		// 只记录路径，避免将 api_key 等查询参数放入可远程读取的日志。
		path := param.Request.URL.Path
		if isManagementPath(path) && param.StatusCode < 400 {
			return ""
		}
		return fmt.Sprintf("[GIN] %s %s %d %s\n", param.Method, path, param.StatusCode, param.Latency)
	}))
	router.Use(gin.Recovery())

	router.Use(proxyMiddleware)

	RegisterManagementRoutes(router)

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

	// 管理命名空间必须在本地结束，不能把管理令牌转发给 TMDB。
	managementMethods := make(map[string][]string)
	for _, route := range router.Routes() {
		if isManagementPath(route.Path) {
			managementMethods[route.Path] = append(managementMethods[route.Path], route.Method)
		}
	}
	router.NoRoute(func(c *gin.Context) {
		if isManagementPath(c.Request.URL.Path) {
			if methods := managementMethods[c.Request.URL.Path]; len(methods) > 0 {
				c.Header("Allow", strings.Join(methods, ", "))
				c.AbortWithStatusJSON(http.StatusMethodNotAllowed, gin.H{"message": "management method not allowed"})
			} else {
				c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"message": "management endpoint not found"})
			}
			return
		}
		ProxyHandler(c)
	})

	return router
}

func isManagementPath(requestPath string) bool {
	if requestPath == "/management" || strings.HasPrefix(requestPath, "/management/") {
		return true
	}
	// 同时保留 //management、/foo/../management 等路径别名，防止上游
	// 规范化路径后收到本应只用于管理接口的令牌。
	cleanPath := path.Clean(requestPath)
	return cleanPath == "/management" || strings.HasPrefix(cleanPath, "/management/")
}

func proxyMiddleware(c *gin.Context) {
	if !isManagementPath(c.Request.URL.Path) {
		logger.Debug("收到请求: %s %s", c.Request.Method, c.Request.URL.Path)
	}
	c.Next()
}
