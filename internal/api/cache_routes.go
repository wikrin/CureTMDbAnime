package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"curetmdbanime/internal/model"
	"curetmdbanime/internal/processor"
)

func GetLogic(tmdbID int) *model.LogicSeries {
	logicCache := processor.GetSeasonSplitterInstance().GetLogicCache()
	cacheKey := fmt.Sprintf("logic_series_%d", tmdbID)

	if cachedLogic, found := logicCache.Get(cacheKey); found {
		if logic, ok := cachedLogic.(*model.LogicSeries); ok {
			return logic
		}
	}
	return nil
}

// RegisterTVRoutes 注册 TV API 路由
func RegisterCacheRoutes(routerGroup *gin.RouterGroup) {
	// 获取特定剧集详情
	routerGroup.GET("/:tmdb_id/mapping/:season_number/:episode_number", func(ginContext *gin.Context) {
		GetSeriesMapping(ginContext)
	})
}

// GetSeriesMapping 获取指定 TMDB ID 的集数映射
func GetSeriesMapping(ginContext *gin.Context) {
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
	requestKey := model.IntPair{
		Season:  seasonNumber,
		Episode: episodeNumber,
	}
	if cachedLogic := GetLogic(tmdbID); cachedLogic != nil {
		if episodesMap := cachedLogic.OrgMap(); len(episodesMap) > 0 {
			if key, ok := episodesMap[requestKey]; ok {
				ginContext.JSON(http.StatusOK, gin.H{
					"season":  key.Season,
					"episode": key.Episode,
				})
				return
			}
		}
	}
	ginContext.JSON(http.StatusOK, gin.H{})
}
