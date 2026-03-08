package processor

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"

	"curetmdbanime/internal/collection"
	"curetmdbanime/internal/logger"
	"curetmdbanime/internal/model"
	"curetmdbanime/internal/providers"
)

// 逻辑缓存默认过期时间
const (
	logicCacheTTL = 5 * time.Hour
)

var (
	seasonSplitterInstance *SeasonSplitter
	seasonSplitterOnce     sync.Once
)

// 获取 SeasonSplitter 单例
func GetSeasonSplitterInstance() *SeasonSplitter {
	seasonSplitterOnce.Do(func() {
		seasonSplitterInstance = &SeasonSplitter{
			cureTMDb:     providers.NewCureTMDb(),
			bangumiAPI:   providers.NewBangumiAPIClient(),
			upstreamTMDB: GetUpstreamTMDBInstance(),
			logicCache:   cache.New(logicCacheTTL, 10*time.Minute), // 缓存共享于所有请求
		}
	})
	return seasonSplitterInstance
}

// SeasonSplitter 处理剧集季分割逻辑
type SeasonSplitter struct {
	cureTMDb     *providers.CureTMDb
	bangumiAPI   *providers.BangumiAPIClient
	upstreamTMDB *UpstreamTMDB
	logicCache   *cache.Cache
}

// augmentedEpisode 结构，去重时追踪剧集信息及其在可修改结构中的引用
type augmentedEpisode struct {
	Episode           *map[string]any // 剧集数据
	OriginalSeasonIdx int             // 原始 seasons 列表索引
	OriginalEpIdx     int             // 原始 episodes 列表索引
	SeasonNumber      int             // 剧集所属 season_number
}

// 获取内部逻辑缓存实例
func (ss *SeasonSplitter) GetLogicCache() *cache.Cache {
	return ss.logicCache
}

// 获取 LogicSeries (支持缓存)
func (ss *SeasonSplitter) GetLogicSeries(tmdbID int, originalInfo map[string]any, params url.Values) (*model.LogicSeries, *model.ServiceError) {
	cacheKey := fmt.Sprintf("logic_series_%d", tmdbID)

	if cachedLogic, found := ss.logicCache.Get(cacheKey); found {
		if logic, ok := cachedLogic.(*model.LogicSeries); ok {
			logger.Debug("缓存命中: cacheKey=%s", cacheKey)
			return logic, nil
		}
	}

	logger.Debug("缓存未命中, 处理数据: cacheKey=%s", cacheKey)
	logic, err := ss.processTV(tmdbID, originalInfo, params)
	if err != nil {
		return nil, model.NewServiceError(http.StatusInternalServerError, err.Error(), err)
	}
	if logic != nil {
		ss.logicCache.Set(cacheKey, logic, cache.DefaultExpiration)
		logger.Debug("LogicSeries 已缓存: cacheKey=%s", cacheKey)
	}
	return logic, nil
}

// 从 CureTMDb 获取剧集信息
func (ss *SeasonSplitter) fetchCureTMDbEntry(tvShow model.TVShow) (*model.SeriesEntry, error) {
	if res, e := ss.cureTMDb.GetSeasonInfo(tvShow.ID); e == nil && len(res) > 0 {
		var se model.SeriesEntry
		if err := se.Decode(res); err != nil {
			return nil, err
		}
		return &se, nil
	}
	return nil, nil
}

// 从 Bangumi API 获取剧集信息
func (ss *SeasonSplitter) fetchBangumiEntry(tvShow model.TVShow) (*model.SeriesEntry, error) {
	var bangumiResult *model.SeriesEntry
	var bangumiErr error

	isAnime := slices.Contains(tvShow.GenreIds(), 16)
	if isAnime && slices.Contains(tvShow.OriginCountry, "JP") {
		seasonCount := tvShow.NumberOfSeasons

		if seasonCount > 0 && seasonCount < 3 {
			if seasonCount == 2 {
				bangumiItemsFor2Seasons, err := ss.bangumiAPI.Search(*tvShow.OriginalName, tvShow.FindAirdateBySeasonNumber(seasonCount))
				if err != nil {
					logger.Error("Bangumi API 搜索 seasonCount=2 失败: %v", err)
				} else if len(bangumiItemsFor2Seasons) > 0 {
					itemFor2Seasons := bangumiItemsFor2Seasons[0]
					if itemFor2Seasons != nil {
						if id, ok := itemFor2Seasons[providers.BangumiResponseID].(float64); ok {
							resultSort, resultEp, getSortEpErr := ss.bangumiAPI.GetSortAndEp(int(id))
							if getSortEpErr != nil {
								logger.Error("Bangumi API GetSortAndEp 失败: %v", getSortEpErr)
							} else if resultSort != nil && resultEp != nil && *resultSort == *resultEp {
								return nil, nil // 条件满足但无 SeriesEntry 返回
							}
						}
					}
				}
			}

			var airDateStr *string = nil
			if tvShow.FirstAirDate != nil {
				airDateStr = tvShow.FirstAirDate
			}
			bangumiItemsForGeneralSearch, err := ss.bangumiAPI.Search(*tvShow.OriginalName, airDateStr)
			if err != nil {
				logger.Error("Bangumi API 通用搜索季信息 (seasonCount < 3) 失败: %v", err)
			} else if len(bangumiItemsForGeneralSearch) > 0 {
				bangumiResult, err = ss.bangumiAPI.SeasonInfo(bangumiItemsForGeneralSearch[0])
				if err != nil {
					bangumiErr = fmt.Errorf("Bangumi API SeasonInfo 通用搜索后失败 (seasonCount < 3): %w", err)
				}
				return bangumiResult, bangumiErr // 成功获取季信息，提前返回
			}
		}

		var airDate *string = nil
		if tvShow.FirstAirDate != nil {
			airDate = tvShow.FirstAirDate
		}
		if sub, e := ss.bangumiAPI.Search(*tvShow.OriginalName, airDate); e == nil && len(sub) > 0 {
			bangumiResult, bangumiErr = ss.bangumiAPI.SeasonInfo(sub[0])
		} else if e != nil {
			bangumiErr = e
		}
	}
	return bangumiResult, bangumiErr
}

// 顺序从 CureTMDb 和 Bangumi 获取剧集信息
func (ss *SeasonSplitter) fetchSeriesEntriesSequentially(tvShow model.TVShow) (seriesEntry *model.SeriesEntry, err error) {
	seriesEntry, err = ss.fetchCureTMDbEntry(tvShow)
	if seriesEntry != nil && err == nil {
		return seriesEntry, err
	}

	seriesEntry, err = ss.fetchBangumiEntry(tvShow)
	return seriesEntry, err
}

// 处理电视剧信息并构建 LogicSeries
func (ss *SeasonSplitter) processTV(tmdbID int, originalInfo map[string]any, params url.Values) (*model.LogicSeries, error) {
	if originalInfo == nil {
		return nil, nil
	}

	// 将原始数据转换为 model.TVShow
	var tvShow model.TVShow
	if err := tvShow.Decode(originalInfo); err != nil {
		return nil, fmt.Errorf("解码原始电视剧数据到 model.TVShow 失败: %w", err)
	}

	seriesEntry, err := ss.fetchSeriesEntriesSequentially(tvShow)
	if err != nil {
		logger.Error("获取剧集信息失败: %v", err)
		return nil, err
	}
	if seriesEntry == nil {
		return nil, nil
	}

	if !seriesEntry.HasInconsistent(tvShow.Seasons()) {
		return nil, nil
	}

	return ss.buildLogicSeries(tmdbID, tvShow, seriesEntry, params)
}

// 并发从 TMDb 获取指定剧集季详情
func (ss *SeasonSplitter) fetchRawSeasonsConcurrently(tmdbID int, series *model.SeriesEntry, maxSeason int, params url.Values) []map[string]any {
	var rawSeasonsMu sync.Mutex
	rawSeasons := []map[string]any{}
	var fetchWg sync.WaitGroup

	for s := series.MinSeason(); s <= maxSeason; s++ {
		fetchWg.Add(1)
		go func(seasonNum int) {
			defer fetchWg.Done()
			if seasonDetail, err := ss.upstreamTMDB.GetSeasonDetail(tmdbID, seasonNum, params); err == nil && seasonDetail != nil {
				rawSeasonsMu.Lock()
				rawSeasons = append(rawSeasons, seasonDetail)
				rawSeasonsMu.Unlock()
			} else if err != nil {
				logger.Error("获取上游季详情失败: TMDB ID=%d, 季数=%d, 错误=%v", tmdbID, seasonNum, err)
			}
		}(s)
	}

	fetchWg.Wait()
	return rawSeasons
}

// 从原始信息和 SeriesEntry 构建 LogicSeries
func (ss *SeasonSplitter) buildLogicSeries(tmdbID int, tvShow model.TVShow, series *model.SeriesEntry, params url.Values) (*model.LogicSeries, error) {
	logicSeries := &model.LogicSeries{
		Name:        series.Name,
		VoteAverage: *tvShow.VoteAverage,
		PosterPath:  tvShow.PosterPath,
		Overview:    tvShow.Overview,
		Seasons:     []*model.LogicSeason{},
	}

	maxSeason := tvShow.NumberOfSeasons
	// 索引 (含第 0 季)
	rawEpLookup := make(map[string]map[string]any)
	// 获取季详情 (不含第 0 季)
	rawSeasons := ss.fetchRawSeasonsConcurrently(tmdbID, series, maxSeason, params)

	if series.HasAnySpecialSeason() {
		seasonDetail, err := ss.upstreamTMDB.GetSeasonDetail(tmdbID, 0, params)
		if err != nil {
			logger.Error("获取上游特殊季详情失败: TMDB ID=%d, 错误=%v", tmdbID, err)
		}
		if seasonDetail != nil {
			episodesData := getEpisodesAsMapSlice(seasonDetail["episodes"])
			for _, rEp := range episodesData {
				seasonNum := collection.GetInt(rEp, "season_number")
				episodeNum := collection.GetInt(rEp, "episode_number")
				if seasonNum >= 0 {
					key := fmt.Sprintf("S%dE%d", seasonNum, episodeNum)
					rawEpLookup[key] = rEp
				}
			}
		}
	}

	if maxSeason > 1 {
		rawSeasons = RemoveDuplicateEpisodes(rawSeasons)
	}

	RawEps, err := ss.flattenAndSortEpisodes(rawSeasons)
	if err != nil {
		return nil, err
	}
	if RawEps == nil {
		return logicSeries, nil
	}

	// 剧集映射
	mapping := series.EpisodeMapping()
	// 缺失剧集
	missingEps := series.MissingEpisodesBySeason()

	currSIdx := series.MinSeason()
	seasonsName := series.SeasonNames()
	currMissing := missingEps[currSIdx]
	logicEpNum := 0

	for i, ep := range RawEps {

		seasonNum := collection.GetInt(ep, "season_number")
		episodeNum := collection.GetInt(ep, "episode_number")
		if seasonNum != 0 && episodeNum != 0 {
			key := fmt.Sprintf("S%dE%d", seasonNum, episodeNum)
			rawEpLookup[key] = ep
		}

		if len(currMissing) > 0 {
			logicEpNum = currMissing[0]
			currMissing = currMissing[1:]
		} else {
			logicEpNum++
		}

		if _, ok := mapping[currSIdx]; !ok {
			mapping[currSIdx] = make(map[int]*model.EpisodeMap)
		}

		mapping[currSIdx][logicEpNum] = &model.EpisodeMap{
			Order:         logicEpNum,
			EpisodeNumber: &episodeNum,
			SeasonNumber:  &seasonNum,
			Type:          "tv",
			TMDBID:        &tmdbID,
		}

		isLast := i == len(RawEps)-1

		for (func() bool { _, exists := missingEps[currSIdx]; return exists && len(currMissing) == 0 })() || isLast {
			airDate, posterPath := ss.getFirstEpisodeMetadata(rawEpLookup, mapping[currSIdx], params)
			seasonsName := seasonsName[currSIdx]
			logicSeries.AddSeason(&seasonsName, &airDate, currSIdx,
				mapping[currSIdx], map[string]any{"poster_path": posterPath})
			if isLast {
				break
			}
			currSIdx++
			currMissing = missingEps[currSIdx]
			logicEpNum = 0
		}
	}

	return logicSeries, nil
}

// 计算集完整性评分
func completenessScore(episode map[string]any) int {
	score := 0
	if val, ok := episode["name"]; ok && val != nil && val != "" {
		score += 10
	}
	if val, ok := episode["overview"]; ok && val != nil && val != "" {
		score += 8
	}
	if val, ok := episode["still_path"]; ok && val != nil && val != "" {
		score += 5
	}
	// vote_average (float64), 非零表示存在
	if val, ok := episode["vote_average"].(float64); ok && val != 0 {
		score += 3
	} else if val, ok := episode["vote_average"].(int); ok && val != 0 { // 检查 int 类型
		score += 3
	}

	for k, v := range episode {
		// 排除已评分或不计入通用完整度的键
		// season_number, episode_number, id 是合并时特殊处理字段，不参与通用完整度评分
		if k == "name" || k == "overview" || k == "still_path" || k == "vote_average" || k == "air_date" || k == "season_number" || k == "episode_number" || k == "id" {
			continue
		}

		if v != nil {
			if s, isString := v.(string); isString && s == "" {
				// 空字符串不计分
			} else if _, isBool := v.(bool); isBool {
				// 布尔值不计分 (除非特殊需求)
			} else {
				score++
			}
		}
	}
	return score
}

// 根据播出日期和季度号去重，保留最完整剧集并保持顺序
func RemoveDuplicateEpisodes(seasons []map[string]any) []map[string]any {
	// episodesByAirDate: 按 air_date 分组所有剧集，每个 air_date 对应 augmentedEpisode 列表。
	// augmentedEpisode 包含指向 seasons 中实际剧集 map 的指针。
	episodesByAirDate := make(map[string][]*augmentedEpisode)

	// 按 air_date 分组，填充 augmentedEpisode 结构
	for seasonIdx, season := range seasons { // 直接操作输入的 seasons
		seasonNum := collection.GetInt(season, "season_number")

		if episodesList, ok := season["episodes"].([]any); ok {
			for epIdx, epInterface := range episodesList {
				episodeMap, isMap := epInterface.(map[string]any)
				if !isMap {
					continue // 剧集非 map 类型，跳过
				}

				airDate, airDateExists := episodeMap["air_date"].(string)
				if !airDateExists || airDate == "" {
					continue // 无 air_date 剧集不参与去重
				}

				// 创建 augmentedEpisode, Episode 字段指向 seasons 中实际 map
				augmentedEp := &augmentedEpisode{
					Episode:           &episodeMap, // 指针！
					OriginalSeasonIdx: seasonIdx,
					OriginalEpIdx:     epIdx,
					SeasonNumber:      seasonNum,
				}
				episodesByAirDate[airDate] = append(episodesByAirDate[airDate], augmentedEp)
			}
		}
	}

	// slotsToRemove: 存储需移除的原始槽位索引
	slotsToRemove := make(map[struct{ SeasonIdx, EpIdx int }]bool)

	// air_date 组，进行内容合并和标记移除
	for _, group := range episodesByAirDate { // group 是 []*augmentedEpisode
		if len(group) <= 1 {
			continue // 单个剧集，无需去重
		}

		// 检查此 group 是否包含跨季度的剧集
		seasonNumbersInGroup := make(map[int]bool)
		for _, ae := range group {
			seasonNumbersInGroup[ae.SeasonNumber] = true
		}

		if len(seasonNumbersInGroup) == 1 {
			// 所有剧集属同 SeasonNumber，不进行去重合并
			continue
		}

		// 寻找“最佳内容”剧集 (最高分) 和“保留槽位”剧集 (最小 OriginalSeasonIdx/EpIdx)
		// 初始化为组中第一个剧集
		bestContentAugmentedEp := group[0]
		maxScore := completenessScore(*bestContentAugmentedEp.Episode)

		keeperSlotAugmentedEp := group[0]
		minSeasonIdx := keeperSlotAugmentedEp.OriginalSeasonIdx
		minEpIdx := keeperSlotAugmentedEp.OriginalEpIdx

		for i := 1; i < len(group); i++ { // 从第二个剧集开始遍历
			ae := group[i]
			currentScore := completenessScore(*ae.Episode)

			// 最佳内容剧集: 最高分，平分时 OriginalSeasonIdx 最小，其次 OriginalEpIdx 最小
			if currentScore > maxScore {
				maxScore = currentScore
				bestContentAugmentedEp = ae
			} else if currentScore == maxScore {
				if ae.OriginalSeasonIdx < bestContentAugmentedEp.OriginalSeasonIdx { // 优先选择 OriginalSeasonIdx 较小
					bestContentAugmentedEp = ae
				} else if ae.OriginalSeasonIdx == bestContentAugmentedEp.OriginalSeasonIdx {
					if ae.OriginalEpIdx < bestContentAugmentedEp.OriginalEpIdx { // 优先选择 OriginalEpIdx 较小
						bestContentAugmentedEp = ae
					}
				}
			}

			// 保留槽位剧集: OriginalSeasonIdx 最小，其次 OriginalEpIdx 最小
			if ae.OriginalSeasonIdx < minSeasonIdx {
				minSeasonIdx = ae.OriginalSeasonIdx
				minEpIdx = ae.OriginalEpIdx
				keeperSlotAugmentedEp = ae
			} else if ae.OriginalSeasonIdx == minSeasonIdx {
				if ae.OriginalEpIdx < minEpIdx {
					minEpIdx = ae.OriginalEpIdx
					keeperSlotAugmentedEp = ae
				}
			}
		}

		// 合并内容
		// 备份 keeperSlotAugmentedEp 中要保留字段值 (season_number, episode_number, id)
		// bestContentAugmentedEp 字段可能与 keeperSlotAugmentedEp 不同，需保留 keeperSlotAugmentedEp 原始位置信息
		originalSeasonNumber := (*keeperSlotAugmentedEp.Episode)["season_number"]
		originalEpisodeNumber := (*keeperSlotAugmentedEp.Episode)["episode_number"]
		originalID := (*keeperSlotAugmentedEp.Episode)["id"]

		// 复制 bestContentAugmentedEp 所有内容字段到 keeperSlotAugmentedEp 指向的实际剧集 map。
		// 覆盖 keeperSlotAugmentedEp 内容
		for k, v := range *bestContentAugmentedEp.Episode {
			(*keeperSlotAugmentedEp.Episode)[k] = v
		}

		// 恢复 keeperSlotAugmentedEp 原始 season_number, episode_number, id 字段值
		// 确保合并后剧集仍有正确原始位置信息
		(*keeperSlotAugmentedEp.Episode)["season_number"] = originalSeasonNumber
		(*keeperSlotAugmentedEp.Episode)["episode_number"] = originalEpisodeNumber
		(*keeperSlotAugmentedEp.Episode)["id"] = originalID

		// 标记除 keeperSlotAugmentedEp 外所有剧集为移除
		for _, ae := range group {
			// 直接判断 augmentedEpisode 是否为保留槽位剧集
			if ae != keeperSlotAugmentedEp {
				slotsToRemove[struct{ SeasonIdx, EpIdx int }{ae.OriginalSeasonIdx, ae.OriginalEpIdx}] = true
			}
		}
	}

	// 根据 slotsToRemove 构建季度列表
	seasonsWriteIdx := 0 // 跟踪修改后 seasons 列表写入有效季度的位置

	for seasonsReadIdx := range seasons {
		currentSeason := seasons[seasonsReadIdx]
		var filteredEpisodes []map[string]any
		// 遍历当前季度所有剧集
		if episodesList, ok := currentSeason["episodes"].([]any); ok {
			for epIdx, epInterface := range episodesList {
				// 检查剧集槽位是否标记为移除
				if !slotsToRemove[struct{ SeasonIdx, EpIdx int }{seasonsReadIdx, epIdx}] {
					// 确保添加到 filteredEpisodes 为 map[string]any 类型
					if episodeMap, isMap := epInterface.(map[string]any); isMap {
						filteredEpisodes = append(filteredEpisodes, episodeMap)
					}
				}
			}
		}

		// 过滤后该季度仍有剧集
		if len(filteredEpisodes) > 0 {
			// 更新当前季度 episodes 键值为 filteredEpisodes
			currentSeason["episodes"] = filteredEpisodes

			// 若 seasonsReadIdx != seasonsWriteIdx, 表明有季度被跳过 (移除或为空)
			// 需将当前有效季度复制到 seasons[seasonsWriteIdx]
			if seasonsReadIdx != seasonsWriteIdx {
				seasons[seasonsWriteIdx] = currentSeason
			}
			seasonsWriteIdx++ // 递增写入索引
		}
	}

	// 返回修改后 seasons slice 的有效部分
	return seasons[:seasonsWriteIdx]
}

func getEpisodesAsMapSlice(episodesField any) []map[string]any {
	// 优先尝试直接断言为 []map[string]any，这是最理想的情况
	if episodesMapSlice, ok := episodesField.([]map[string]any); ok {
		return episodesMapSlice
	}

	// 如果不是，尝试断言为 []any，然后逐个元素转换为 map[string]any
	if episodesAny, ok := episodesField.([]any); ok {
		var result []map[string]any
		for _, ep := range episodesAny {
			if epMap, ok := ep.(map[string]any); ok {
				result = append(result, epMap)
			}
			// 如果 []any 中有非 map[string]any 的元素，这里会跳过。
			// 根据实际需求，也可以在此处记录日志或返回错误。
		}
		return result
	}

	// 如果都不是期望的切片类型，返回 nil。
	return nil
}

// 展平并按播出日期排序所有季剧集
func (ss *SeasonSplitter) flattenAndSortEpisodes(rawSeasons []map[string]any) ([]map[string]any, error) {
	var allEpisodes []map[string]any

	for _, season := range rawSeasons {
		if sn, ok := season["season_number"].(float64); ok {
			seasonNumberInt := int(sn)

			// 使用辅助函数统一获取 episodes 列表
			episodes := getEpisodesAsMapSlice(season["episodes"])

			// 遍历 episodes 列表，添加季号并收集所有剧集
			for _, ep := range episodes {
				ep["season_number"] = seasonNumberInt
				allEpisodes = append(allEpisodes, ep)
			}
		}
	}

	sort.Slice(allEpisodes, func(i, j int) bool {
		dateI := collection.GetString(allEpisodes[i], "air_date")
		dateJ := collection.GetString(allEpisodes[j], "air_date")
		// 都有日期, 正常比较
		if dateI != "" && dateJ != "" {
			return dateI < dateJ
		}
		// 仅一个为空，有日期排前面
		if dateI == "" && dateJ != "" {
			return false // dateI (空) 排在后面
		}
		if dateI != "" && dateJ == "" {
			return true // dateJ (空) 排在后面
		}

		// 都为空，保持相对顺序
		return false
	})

	return allEpisodes, nil
}

func (ss *SeasonSplitter) getFirstEpisodeMetadata(rawEpLookup map[string]map[string]any,
	epMap map[int]*model.EpisodeMap, params url.Values) (airDate string, posterPath *string) {
	if len(epMap) == 0 {
		return "", nil
	}

	// 获取 map 的所有键
	keys := make([]int, 0, len(epMap))
	for k := range epMap {
		keys = append(keys, k)
	}
	minKey := slices.Min(keys)
	if minKey == -1 {
		return "", nil
	}

	firstEpMap := epMap[minKey]

	// 根据剧集类型获取元数据
	if firstEpMap.SeasonNum() < 0 && firstEpMap.TMDBID != nil {
		// 处理电影类型
		return ss.getMovieMetadata(*firstEpMap.TMDBID, params)
	}

	// 处理剧集类型
	return ss.getEpisodeMetadata(rawEpLookup, firstEpMap)
}

// 获取电影类型的元数据
func (ss *SeasonSplitter) getMovieMetadata(tmdbID int, params url.Values) (airDate string, posterPath *string) {
	upstreamMovie, err := ss.upstreamTMDB.GetMovieDetail(tmdbID, params)
	if err != nil {
		logger.Error("[ERROR] 获取电影详情失败: %v", err)
		return "", nil
	}

	if upstreamMovie != nil {
		return collection.GetString(upstreamMovie, "air_date"),
			collection.GetStringPtr(upstreamMovie, "still_path")
	}

	return "", nil
}

// 获取剧集类型的元数据
func (ss *SeasonSplitter) getEpisodeMetadata(rawEpLookup map[string]map[string]any,
	firstEpMap *model.EpisodeMap,
) (airDate string, posterPath *string) {
	lookupKey := fmt.Sprintf("S%dE%d", *firstEpMap.SeasonNumber, *firstEpMap.EpisodeNumber)

	if foundEp, ok := rawEpLookup[lookupKey]; ok {
		return collection.GetString(foundEp, "air_date"),
			collection.GetStringPtr(foundEp, "still_path")
	}

	return "", nil
}
