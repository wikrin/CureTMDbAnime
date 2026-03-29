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

// RegisterCacheRoutes 注册缓存 API 路由
func RegisterCacheRoutes(routerGroup *gin.RouterGroup) {
	routerGroup.GET("/mapping/:tmdb_id", func(ginContext *gin.Context) {
		GetSeriesMappings(ginContext)
	})
}

// GetSeriesMappings 获取指定 TMDB ID 的整剧集数映射
func GetSeriesMappings(ginContext *gin.Context) {
	tmdbIDStr := ginContext.Param("tmdb_id")

	tmdbID, err := strconv.Atoi(tmdbIDStr)
	if err != nil {
		handleError(ginContext, model.NewServiceError(http.StatusBadRequest, "无效的 TMDB ID 格式", err))
		return
	}

	response := map[string]map[string]model.IntPair{}
	if cachedLogic := GetLogic(tmdbID); cachedLogic != nil {
		for source, target := range cachedLogic.OrgMap() {
			seasonKey := strconv.Itoa(source.Season)
			episodeKey := strconv.Itoa(source.Episode)
			if _, ok := response[seasonKey]; !ok {
				response[seasonKey] = map[string]model.IntPair{}
			}
			response[seasonKey][episodeKey] = target
		}
	}

	ginContext.JSON(http.StatusOK, response)
}
