package service

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"sync"

	"curetmdbanime/internal/collection"
	"curetmdbanime/internal/logger"
	"curetmdbanime/internal/model"
	"curetmdbanime/internal/processor"
)

// 负责处理电视剧相关的业务逻辑
type TVService struct {
	splitter *processor.SeasonSplitter // 剧集季分割器
	upstream *processor.UpstreamTMDB   // 上游 TMDb API 客户端
}

// 创建并返回一个 TVService 实例
func NewTVService() *TVService {
	return &TVService{
		splitter: processor.GetSeasonSplitterInstance(), // 获取季分割器单例
		upstream: processor.GetUpstreamTMDBInstance(),   // 获取上游 TMDb 客户端单例
	}
}

// 确保给定 tmdbID 的 LogicSeries 已加载到缓存
// 这是处理自定义剧集逻辑的前提
func (s *TVService) ensureLogicLoaded(tmdbID int, params url.Values) *model.ServiceError {
	// 尝试从上游获取原始信息
	originalTVShowData, err := s.upstream.GetTVDetail(tmdbID, params)
	if err != nil {
		logger.Error("upstream.GetTVDetail 获取上游电视剧详情失败: %v", err)
		return err
	}
	if originalTVShowData == nil {
		return model.NewServiceError(http.StatusNotFound, fmt.Sprintf("未找到 TMDB ID %d 的电视剧详情", tmdbID), nil)
	}

	_, err = s.splitter.GetLogicSeries(tmdbID, originalTVShowData, params)
	if err != nil {
		logger.Warn("无法为剧集加载逻辑: TMDB ID=%d, 错误=%v", tmdbID, err)
		return err
	}
	return nil
}

// 获取电视剧详情，并应用自定义逻辑
func (s *TVService) GetTVDetail(tmdbID int, params url.Values) (*model.TVShow, *model.ServiceError) {
	originalTVShowData, err := s.upstream.GetTVDetail(tmdbID, params)
	if err != nil {
		logger.Error("upstream.GetTVDetail 获取上游电视剧详情失败: %v", err)
		return nil, err
	}
	if originalTVShowData == nil {
		return nil, model.NewServiceError(http.StatusNotFound, fmt.Sprintf("未找到 TMDB ID %d 的电视剧详情", tmdbID), nil)
	}

	// 尝试获取自定义剧集逻辑
	logicSeriesData, err := s.splitter.GetLogicSeries(tmdbID, originalTVShowData, params)
	if err != nil {
		logger.Warn("获取剧集逻辑失败: TMDB ID=%d, 错误=%v", tmdbID, err)
		// 如果逻辑获取失败，继续使用原始数据，仅记录警告
	}

	// 合并原始 TMDb 数据和自定义逻辑，转换为 model.TVShow
	finalTVShow, err := s.applyTVLogicAndTransform(tmdbID, originalTVShowData, logicSeriesData)
	if err != nil {
		logger.Error("应用剧集逻辑并转换电视剧信息失败: TMDB ID=%d, 错误=%v", tmdbID, err)
		return nil, err
	}

	return finalTVShow, nil
}

// 合并原始 TMDb 电视剧数据和自定义 LogicSeries 数据
// 转换为最终的 model.TVShow 结构
func (s *TVService) applyTVLogicAndTransform(tmdbID int, originalTVShowData map[string]any, logicSeriesData *model.LogicSeries) (*model.TVShow, *model.ServiceError) {
	var tvShow model.TVShow
	if err := tvShow.Decode(originalTVShowData); err != nil {
		return nil, model.NewServiceError(http.StatusInternalServerError, "解码原始电视剧数据到 model.TVShow 失败", err)
	}

	// 如果存在自定义逻辑，则应用
	if logicSeriesData != nil {
		name := *tvShow.Name
		if logicSeriesData.Name != nil {
			name = *logicSeriesData.Name
		}
		logger.Info("剧集信息已重写: TMDB ID=%d, 剧集名称=%s", tmdbID, name)
		s.remapTVDetailEpisodeReferences(tvShow.Other, logicSeriesData.OrgMap())

		if logicSeriesData.Name != nil {
			tvShow.Name = logicSeriesData.Name
		}

		mergedSeasons := []model.Season{}
		season0 := s.findSeasonByNumber(tvShow.Seasons, 0)
		if season0 != nil {
			mergedSeasons = append(mergedSeasons, *season0)
		}

		for _, logicSeason := range logicSeriesData.Seasons {
			newSeason := model.Season{
				AirDate:      logicSeason.AirDate,
				EpisodeCount: logicSeason.EpisodeCount(),
				ID:           tmdbID*1000 + logicSeason.SeasonNumber,
				Name:         logicSeason.Name,
				Overview:     logicSeason.Overview,
				PosterPath:   logicSeason.PosterPath,
				SeasonNumber: logicSeason.SeasonNumber,
				VoteAverage:  logicSeason.VoteAverage,
			}
			mergedSeasons = append(mergedSeasons, newSeason)
		}

		tvShow.SetSeasons(mergedSeasons)
	}

	return &tvShow, nil
}

// 获取季详情，并应用自定义逻辑（如果可用）
func (s *TVService) GetSeasonDetail(tmdbID, seasonNumber int, params url.Values) (*model.Season, *model.ServiceError) {
	err := s.ensureLogicLoaded(tmdbID, params)
	if err != nil {
		logger.Warn("无法为剧集加载逻辑: TMDB ID=%d, 错误=%v", tmdbID, err)
		return nil, err
	}

	// 尝试从缓存中获取剧集逻辑
	logicCacheEntry, found := s.splitter.GetLogicCache().Get(fmt.Sprintf("logic_series_%d", tmdbID))
	if found {
		logicSeries := logicCacheEntry.(*model.LogicSeries)

		// 尝试合并并返回逻辑季信息
		logicSeason, mergeErr := s.fetchAndMergeLogicSeasonEpisodes(tmdbID, seasonNumber, params, logicSeries)
		if mergeErr != nil {
			logger.Warn("合并逻辑季剧集失败，回退到上游数据: TMDB ID=%d, 季号=%d, 错误=%v", tmdbID, seasonNumber, mergeErr)
			return s.getUpstreamSeasonDetail(tmdbID, seasonNumber, params)
		}
		if logicSeason != nil {
			return logicSeason, nil
		}
	}

	// 如果没有自定义逻辑，则获取上游季详情
	return s.getUpstreamSeasonDetail(tmdbID, seasonNumber, params)
}

// 获取原始季的剧集数据
// 根据 LogicSeries 进行剧集重映射和聚合，生成逻辑季的 model.Season
func (s *TVService) fetchAndMergeLogicSeasonEpisodes(tmdbID, seasonNumber int, params url.Values, logicSeries *model.LogicSeries) (*model.Season, error) {
	logicSeasonInfo := logicSeries.SeasonInfo(seasonNumber)
	if logicSeasonInfo == nil {
		return nil, nil // 未找到对应的逻辑季信息
	}

	logger.Info("提供虚拟季度: TMDB ID=%d, 季号=%d", tmdbID, seasonNumber)

	rawEpisodesData := []map[string]any{}
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, episodeMap := range logicSeasonInfo.UniqueEntry() {
		wg.Add(1)
		if episodeMap.Type == "tv" {
			go func(orgSeasonNum int) {
				defer wg.Done()
				upstreamSeasonEpisodes, upstreamErr := s.upstream.GetSeasonDetail(tmdbID, orgSeasonNum, params)
				if upstreamErr != nil {
					logger.Warn("获取上游季数据失败: 原始季号=%d, 错误=%v", orgSeasonNum, upstreamErr)
					return
				}
				if upstreamSeasonEpisodes != nil {
					if episodes, ok := upstreamSeasonEpisodes["episodes"].([]any); ok {
						mu.Lock()
						for _, episodeData := range episodes {
							if episodeMap, ok := episodeData.(map[string]any); ok {
								rawEpisodesData = append(rawEpisodesData, episodeMap)
							}
						}
						mu.Unlock()
					}
				}
			}(*episodeMap.SeasonNumber)
		} else {
			go func(em *model.EpisodeMap) {
				defer wg.Done()
				if em.TMDBID == nil {
					logger.Error("fetchAndMergeLogicSeasonEpisodes: episodeMap.TMDBID 对于类型 '%s' 为 nil. 原始映射: %+v. 跳过电影获取。", em.Type, em)
					return
				}
				movieID := *em.TMDBID
				upstreamMovie, upstreamErr := s.upstream.GetMovieDetail(movieID, params)
				if upstreamErr != nil {
					logger.Warn("获取上游电影数据失败: MovieID=%d, 错误=%v", movieID, upstreamErr)
					return
				}
				if upstreamMovie != nil {
					mu.Lock()
					rawEpisodesData = append(rawEpisodesData, upstreamMovie)
				}
				mu.Unlock()
			}(episodeMap)
		}
	}
	wg.Wait()

	finalEpisodes := []model.Episode{}
	revMap := logicSeasonInfo.OrgMap()

	for _, rawEpisode := range rawEpisodesData {
		if len(finalEpisodes) >= len(revMap) {
			break
		}
		var episode model.Episode
		if err := episode.Decode(rawEpisode); err != nil {
			logger.Error("解码原始剧集数据到 model.Episode 失败: %v", err)
		}
		if intPair, ok := revMap[episode.MappingKey()]; ok {
			episode.SeasonNumber = seasonNumber
			episode.EpisodeNumber = intPair.Episode
			finalEpisodes = append(finalEpisodes, episode)
		}
	}

	sort.Slice(finalEpisodes, func(i, j int) bool {
		return finalEpisodes[i].EpisodeNumber < finalEpisodes[j].EpisodeNumber
	})

	return &model.Season{
		AirDate:      logicSeasonInfo.AirDate,
		Episodes:     finalEpisodes,
		Name:         logicSeasonInfo.Name,
		Overview:     logicSeasonInfo.Overview,
		ID:           tmdbID*1000 + seasonNumber,
		PosterPath:   logicSeasonInfo.PosterPath,
		SeasonNumber: seasonNumber,
	}, nil
}

// 从上游 TMDb 获取季详情
func (s *TVService) getUpstreamSeasonDetail(tmdbID, seasonNumber int, params url.Values) (*model.Season, *model.ServiceError) {
	upstreamSeasonData, err := s.upstream.GetSeasonDetail(tmdbID, seasonNumber, params)
	if err != nil {
		logger.Error("获取上游季详情失败: TMDB ID=%d, 季号=%d, 错误=%v", tmdbID, seasonNumber, err)
		return nil, err
	}
	if upstreamSeasonData == nil {
		return nil, model.NewServiceError(http.StatusNotFound, fmt.Sprintf("未找到 TMDB ID %d, 季号 %d 的季详情", tmdbID, seasonNumber), nil)
	}

	var season model.Season
	if err := season.Decode(upstreamSeasonData); err != nil {
		return nil, model.NewServiceError(http.StatusInternalServerError, "解码上游季信息到 model.Season 失败", err)
	}
	return &season, nil
}

// 获取剧集详情，并应用自定义逻辑（如果可用）
func (s *TVService) GetEpisodeDetail(tmdbID, seasonNumber, episodeNumber int, params url.Values) (*model.Episode, *model.ServiceError) {
	err := s.ensureLogicLoaded(tmdbID, params)
	if err != nil {
		logger.Warn("无法为剧集加载逻辑: TMDB ID=%d, 错误=%v", tmdbID, err)
	}

	// 尝试从缓存中获取剧集逻辑
	logicCacheEntry, found := s.splitter.GetLogicCache().Get(fmt.Sprintf("logic_series_%d", tmdbID))
	if found {
		logicSeries := logicCacheEntry.(*model.LogicSeries)

		// 尝试获取并应用逻辑剧集信息
		logicEpisode, serviceErr := s.fetchAndApplyLogicEpisodeDetail(tmdbID, seasonNumber, episodeNumber, params, logicSeries)
		if serviceErr != nil {
			logger.Warn("获取并应用逻辑剧集详情失败: TMDB ID=%d, 季号=%d, 剧集号=%d, 错误=%v", tmdbID, seasonNumber, episodeNumber, serviceErr)
		}
		if logicEpisode != nil {
			return logicEpisode, nil
		}
	}
	// 获取上游剧集详情
	return s.getUpstreamEpisodeDetail(tmdbID, seasonNumber, episodeNumber, params)
}

// 根据 LogicSeries 的剧集映射
// 获取上游剧集详情，并转换为逻辑剧集信息
func (s *TVService) fetchAndApplyLogicEpisodeDetail(tmdbID int, seasonNumber int, episodeNumber int, params url.Values, logicSeries *model.LogicSeries) (*model.Episode, *model.ServiceError) {
	logicSeasonInfo := logicSeries.SeasonInfo(seasonNumber)
	if logicSeasonInfo != nil {
		if episodeMapping, ok := logicSeasonInfo.EpisodesMap[episodeNumber]; ok {
			logger.Info("提供虚拟单集: TMDB ID=%d, 季号=%d, 剧集号=%d", tmdbID, seasonNumber, episodeNumber)

			if episodeMapping.Type == "movie" && episodeMapping.TMDBID != nil {
				logger.Info("虚拟单集: TMDB ID=%d, 季号=%d, 剧集号=%d, 重定向至电影: TMDB ID=%d", tmdbID, seasonNumber, episodeNumber, *episodeMapping.TMDBID)
			}

			upstreamEpisode, err := s.getUpstreamEpisodeDetail(tmdbID, episodeMapping.SeasonNum(), episodeMapping.EpisodeNum(), params)
			if err != nil {
				return nil, err
			}
			upstreamEpisode.SeasonNumber = seasonNumber
			upstreamEpisode.EpisodeNumber = episodeNumber
			return upstreamEpisode, nil
		}
	}
	return nil, nil // 未找到自定义逻辑
}

// 从上游 TMDb 获取剧集详情
func (s *TVService) getUpstreamEpisodeDetail(tmdbID int, seasonNumber int, episodeNumber int, params url.Values) (*model.Episode, *model.ServiceError) {
	var upstreamData map[string]any
	var err *model.ServiceError

	if seasonNumber < 0 && episodeNumber > 0 {
		upstreamData, err = s.upstream.GetMovieDetail(episodeNumber, params)
	} else {
		upstreamData, err = s.upstream.GetEpisodeDetail(tmdbID, seasonNumber, episodeNumber, params)
	}
	if err != nil {
		logger.Error("获取上游剧集详情失败: TMDB ID=%d, 季号=%d, 剧集号=%d, 错误=%v", tmdbID, seasonNumber, episodeNumber, err)
		return nil, err
	}
	if upstreamData == nil {
		return nil, model.NewServiceError(http.StatusNotFound, fmt.Sprintf("未找到 TMDB ID %d, 季号 %d, 剧集号 %d 的剧集详情", tmdbID, seasonNumber, episodeNumber), nil)
	}

	var episode model.Episode
	if err := episode.Decode(upstreamData); err != nil {
		return nil, model.NewServiceError(http.StatusInternalServerError, "解码上游剧集信息到 model.Episode 失败", err)
	}
	return &episode, nil
}

// 重映射电视剧详情中的剧集引用字段
// 根据自定义逻辑映射表，更新季号和集号
func (s *TVService) remapTVDetailEpisodeReferences(other map[string]any, orgMap map[model.IntPair]model.IntPair) {
	if len(other) == 0 || len(orgMap) == 0 {
		return
	}

	// 定义需要重映射的字段列表
	fieldsToRemap := []string{
		"last_episode_to_air",
		"next_episode_to_air",
	}

	for _, field := range fieldsToRemap {
		remapSingleEpisodeField(other, field, orgMap)
	}
}

// 重映射单个剧集引用字段
// 如果字段存在且其原始季号/集号在映射表中，则更新为逻辑季号/集号
// 返回是否成功进行了重映射
func remapSingleEpisodeField(other map[string]any, key string, orgMap map[model.IntPair]model.IntPair) bool {
	rawEpisode, exists := other[key]
	if !exists {
		logger.Debug("字段 %s 不存在", key)
		return false
	}

	episodeMap, ok := rawEpisode.(map[string]any)
	if !ok {
		logger.Debug("%s 字段类型异常，期望 map[string]any，实际=%T", key, rawEpisode)
		return false
	}

	seasonNumber := collection.GetInt(episodeMap, "season_number")
	episodeNumber := collection.GetInt(episodeMap, "episode_number")
	if seasonNumber < 0 || episodeNumber < 0 {
		return false
	}

	if mapped, found := orgMap[model.IntPair{Season: seasonNumber, Episode: episodeNumber}]; found {
		episodeMap["season_number"] = mapped.Season
		episodeMap["episode_number"] = mapped.Episode
		logger.Info("重映射剧集引用字段 `%s` S%02dE%02d -> S%02dE%02d", key, seasonNumber, episodeNumber, mapped.Season, mapped.Episode)
		return true
	}

	return false
}

// 在 Season 切片中按季号查找季
func (s *TVService) findSeasonByNumber(seasons []model.Season, number int) *model.Season {
	for i := range seasons {
		if seasons[i].SeasonNumber == number {
			return &seasons[i]
		}
	}
	return nil
}
