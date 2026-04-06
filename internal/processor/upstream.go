package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"curetmdbanime/internal/collection"
	"curetmdbanime/internal/config"
	"curetmdbanime/internal/logger"
	"curetmdbanime/internal/model"
	"curetmdbanime/internal/net"
)

// 定义 UpstreamTMDB 的单例实例和 Once 对象
var (
	upstreamTMDBInstance *UpstreamTMDB
	once                 sync.Once
)

// 负责与 TMDB API 交互
type UpstreamTMDB struct {
	httpClient *net.HTTPClientWrapper
}

// 返回 UpstreamTMDB 单例
func GetUpstreamTMDBInstance() *UpstreamTMDB {
	once.Do(func() {
		upstreamTMDBInstance = &UpstreamTMDB{
			httpClient: net.GetHTTPClientWrapper(),
		}
	})
	return upstreamTMDBInstance
}

// 从 TMDB API 获取数据并反序列化为 map
func (u *UpstreamTMDB) getTMDBData(path string, params url.Values) (map[string]any, *model.ServiceError) {
	fullURL := fmt.Sprintf("%s/3/%s", config.AppSettings.TmdbAPIURL, path)
	if len(params) > 0 {
		fullURL += "?" + params.Encode()
	}

	resp, err := u.httpClient.GetRes(context.Background(), fullURL, nil)
	if resp == nil {
		return nil, model.NewServiceError(http.StatusInternalServerError, "HTTP 响应为空", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := net.ReadResponseBody(resp)
	if err != nil {
		return nil, model.NewServiceError(http.StatusInternalServerError, "读取响应体错误", err)
	}

	var result map[string]any
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, model.NewServiceError(http.StatusInternalServerError, "解析 JSON 响应错误", err)
	}

	if resp.StatusCode != http.StatusOK {
		var se model.ServiceError
		if err = se.Decode(result); err != nil {
			return nil, model.NewServiceError(http.StatusInternalServerError, "结构体转换错误", err)
		}
		se.Code = resp.StatusCode
		return nil, &se
	}

	logger.Debug("UpstreamTMDB: %v", result)
	return result, nil
}

// 获取指定 TMDB ID 电视剧详情
func (u *UpstreamTMDB) GetTVDetail(tmdbID int, params url.Values) (map[string]any, *model.ServiceError) {
	path := fmt.Sprintf("tv/%d", tmdbID)
	return u.getTMDBData(path, params)
}

// 获取指定 TMDB ID 电视剧的指定季度详情
func (u *UpstreamTMDB) GetSeasonDetail(tmdbID, seasonNumber int, params url.Values) (map[string]any, *model.ServiceError) {
	path := fmt.Sprintf("tv/%d/season/%d", tmdbID, seasonNumber)
	return u.getTMDBData(path, params)
}

// 获取指定电视剧特定季度特定剧集详情
func (u *UpstreamTMDB) GetEpisodeDetail(tmdbID int, seasonNumber int, episodeNumber int, params url.Values) (map[string]any, *model.ServiceError) {
	path := fmt.Sprintf("tv/%d/season/%d/episode/%d", tmdbID, seasonNumber, episodeNumber)
	return u.getTMDBData(path, params)
}

// 获取指定 TMDB ID 电影详情
func (u *UpstreamTMDB) GetMovieDetail(tmdbID int, params url.Values) (map[string]any, *model.ServiceError) {
	path := fmt.Sprintf("movie/%d", tmdbID)
	result, err := u.getTMDBData(path, params)
	if err != nil {
		return nil, err
	}
	if result != nil {
		collection.RenameKeysInPlace(
			result,
			map[string]string{
				"release_date":  "air_date",
				"title":         "name",
				"backdrop_path": "still_path",
			},
		)
		result["season_number"] = -1
		result["episode_number"] = tmdbID
	}
	return result, nil
}
