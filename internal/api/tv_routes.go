package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"curetmdbanime/internal/logger"
	"curetmdbanime/internal/model"
	"curetmdbanime/internal/service"
)

// 创建并返回 TVService 实例
func createTVService() *service.TVService {
	return service.NewTVService()
}

// 处理 ServiceError 并发送 JSON 错误响应
func handleError(ginContext *gin.Context, serviceErr *model.ServiceError) {
	if serviceErr == nil {
		return // 没有错误, 不处理
	}

	logger.Error("API 错误: 代码=%d, 消息=%s, 错误=%v", serviceErr.Code, serviceErr.Message, serviceErr.Err)

	statusCode := serviceErr.Code
	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}

	errorResponse := model.NewErrorResponse(false, serviceErr.Code, serviceErr.Message)
	ginContext.JSON(statusCode, errorResponse)
}

// 注册 TV API 路由
func RegisterTVRoutes(routerGroup *gin.RouterGroup) {
	// 获取特定剧集详情
	routerGroup.GET("/:tmdb_id", func(ginContext *gin.Context) {
		GetTVDetails(ginContext, createTVService())
	})
	// 获取特定剧集某一季度详情
	routerGroup.GET("/:tmdb_id/season/:season_number", func(ginContext *gin.Context) {
		GetSeasonDetails(ginContext, createTVService())
	})
	// 获取特定剧集某一季度某一单集详情
	routerGroup.GET("/:tmdb_id/season/:season_number/episode/:episode_number", func(ginContext *gin.Context) {
		GetEpisodeDetails(ginContext, createTVService())
	})
}

// 获取指定 TMDB ID 的 TV 详情
func GetTVDetails(ginContext *gin.Context, tvService *service.TVService) {
	tmdbIDStr := ginContext.Param("tmdb_id")
	tmdbID, err := strconv.Atoi(tmdbIDStr)
	if err != nil {
		handleError(ginContext, model.NewServiceError(http.StatusBadRequest, "无效的 TMDB ID 格式", err))
		return
	}

	logger.Info("API 请求: 获取剧集详情, TMDB ID: %d", tmdbID)

	params := ginContext.Request.URL.Query()
	tvDetails, serviceErr := tvService.GetTVDetail(ginContext.Request.Context(), tmdbID, params)
	if serviceErr != nil {
		handleError(ginContext, serviceErr)
		return // 确保在处理服务层错误后立即返回
	}
	if tvDetails == nil {
		handleError(ginContext, model.NewServiceError(http.StatusNotFound, fmt.Sprintf("未找到 TMDB ID 为 %d 的剧集详情", tmdbID), nil))
		return
	}
	ginContext.JSON(http.StatusOK, tvDetails)
}

// 获取指定 TMDB ID 和季度的详情
func GetSeasonDetails(ginContext *gin.Context, tvService *service.TVService) {
	tmdbIDStr := ginContext.Param("tmdb_id")
	seasonNumberStr := ginContext.Param("season_number")

	tmdbID, err := strconv.Atoi(tmdbIDStr)
	if err != nil {
		handleError(ginContext, model.NewServiceError(http.StatusBadRequest, "无效的 TMDB ID 格式", err))
		return
	}
	seasonNumber, err := strconv.Atoi(seasonNumberStr)
	if err != nil {
		handleError(ginContext, model.NewServiceError(http.StatusBadRequest, "无效的季度号格式", err))
		return
	}

	logger.Info("API 请求: 获取季度详情, TMDB ID: %d, 季号: %d", tmdbID, seasonNumber)

	params := ginContext.Request.URL.Query()
	seasonDetails, serviceErr := tvService.GetSeasonDetail(ginContext.Request.Context(), tmdbID, seasonNumber, params)
	if serviceErr != nil {
		handleError(ginContext, serviceErr)
		return // 确保在处理服务层错误后立即返回
	}
	if seasonDetails == nil {
		handleError(ginContext, model.NewServiceError(http.StatusNotFound, fmt.Sprintf("未找到 TMDB ID 为 %d, 季号为 %d 的季详情", tmdbID, seasonNumber), nil))
		return
	}
	ginContext.JSON(http.StatusOK, seasonDetails)
}

// 获取指定 TMDB ID、季度和剧集的详情
func GetEpisodeDetails(ginContext *gin.Context, tvService *service.TVService) {
	tmdbIDStr := ginContext.Param("tmdb_id")
	seasonNumberStr := ginContext.Param("season_number")
	episodeNumberStr := ginContext.Param("episode_number")

	tmdbID, err := strconv.Atoi(tmdbIDStr)
	if err != nil {
		handleError(ginContext, model.NewServiceError(http.StatusBadRequest, "无效的 TMDB ID 格式", err))
		return
	}
	seasonNumber, err := strconv.Atoi(seasonNumberStr)
	if err != nil {
		handleError(ginContext, model.NewServiceError(http.StatusBadRequest, "无效的季度号格式", err))
		return
	}
	episodeNumber, err := strconv.Atoi(episodeNumberStr)
	if err != nil {
		handleError(ginContext, model.NewServiceError(http.StatusBadRequest, "无效的剧集号格式", err))
		return
	}

	logger.Info("API 请求: 获取单集详情, TMDB ID: %d, 季号: %d, 剧集号: %d", tmdbID, seasonNumber, episodeNumber)

	params := ginContext.Request.URL.Query()
	episodeDetails, serviceErr := tvService.GetEpisodeDetail(ginContext.Request.Context(), tmdbID, seasonNumber, episodeNumber, params)
	if serviceErr != nil {
		handleError(ginContext, serviceErr)
		return // 确保在处理服务层错误后立即返回
	}
	if episodeDetails == nil {
		handleError(ginContext, model.NewServiceError(http.StatusNotFound, fmt.Sprintf("未找到 TMDB ID 为 %d, 季号为 %d, 剧集号为 %d 的剧集详情", tmdbID, seasonNumber, episodeNumber), nil))
		return
	}
	ginContext.JSON(http.StatusOK, episodeDetails)
}
