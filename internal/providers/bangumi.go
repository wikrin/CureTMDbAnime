package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"

	"curetmdbanime/internal/logger"
	"curetmdbanime/internal/model"
	"curetmdbanime/internal/net"
)

const (
	BangumiAPIBaseURL          = "https://api.bgm.tv/"
	BangumiAPISearchEndpoint   = "v0/search/subjects"
	BangumiAPIDetailEndpoint   = "v0/subjects/%d"
	BangumiAPISubjectsEndpoint = "v0/subjects/%d/subjects"
	BangumiAPIEpisodesEndpoint = "v0/episodes?subject_id=%d"

	BangumiSearchKeyword    = "keyword"
	BangumiSearchSort       = "sort"
	BangumiSearchSortMatch  = "match"
	BangumiSearchFilter     = "filter"
	BangumiSearchType       = "type"
	BangumiSearchTypeAnime  = 2
	BangumiSearchAirDate    = "air_date"
	BangumiResponseDataKey  = "data"
	BangumiResponseRelation = "relation"
	BangumiResponseID       = "id"
	BangumiRelationSequel   = "续集"
	BangumiPlatform         = "platform"
	BangumiPlatformMovie    = "剧场版"
	BangumiNameCN           = "name_cn"
	BangumiName             = "name"
	BangumiEpisodes         = "eps"
	BangumiTotalEpisodes    = "total_episodes"
	BangumiSortKey          = "sort"
	BangumiEpKey            = "ep"
)

const (
	DefaultEpisodeCount   = 12
	DefaultSeasonNumber   = 1
	SearchDateRangeOffset = 10
)

const (
	BangumiCacheTTL             = 6 * time.Hour
	BangumiCacheCleanupInterval = 10 * time.Minute
)

const (
	ContentTypeHeader = "Content-Type"
	ApplicationJSON   = "application/json"
)

const (
	ErrMsgJSONEncodeRequestFailed            = "JSON 请求体编码错误"
	ErrMsgBangumiAPIRequestFailed            = "Bangumi API 请求错误"
	ErrMsgBangumiAPIRequestStatusCode        = "Bangumi API 请求失败, 状态码: %d"
	ErrMsgReadResponseBodyFailed             = "读取响应体错误"
	ErrMsgJSONDecodeResponseFailed           = "JSON 响应体解码错误"
	LogMsgResponseContent                    = "响应内容: %s"
	LogMsgParseAirDateFailed                 = "解析播出日期 %s 错误"
	ErrMsgSearchResponseFormat               = "搜索响应格式错误: 预期为 map[string]any, 实际为 %T"
	ErrMsgSearchDataFieldFormat              = "搜索响应中 'data' 字段格式错误: 预期为 []any, 实际为 %T"
	ErrMsgSearchDataItemFormat               = "搜索响应中的数据项格式错误: 预期为 map[string]any, 实际为 %T"
	ErrMsgDetailResponseFormat               = "详情响应格式错误: 预期为 map[string]any, 实际为 %T"
	ErrMsgFetchSubjectsFailed                = "获取条目 %d 的相关条目错误"
	ErrMsgSubjectsResponseFormat             = "相关条目响应格式错误: 预期为 []any 或 {\"\": []any}, 实际为 %T"
	ErrMsgSubjectsDataItemFormat             = "相关条目数据项格式错误: 预期为 map[string]any, 实际为 %T"
	ErrMsgFetchEpisodesFailed                = "获取条目 %d 的剧集列表错误"
	ErrMsgEpisodesResponseFormat             = "剧集列表响应格式错误: 预期为 map[string]any, 实际为 %T"
	ErrMsgEpisodesDataFieldFormat            = "剧集列表响应中 'data' 字段格式错误: 预期为 []any, 实际为 %T"
	ErrMsgEpisodesDataItemFormat             = "剧集列表数据项格式错误: 预期为 map[string]any, 实际为 %T"
	ErrMsgGetAllSequelsFailed                = "获取所有续集时出错"
	ErrMsgGetDetailFailed                    = "获取 Bangumi ID %d 的详情时出错"
	ErrMsgGetSortAndEpFailed                 = "获取 Bangumi ID %d 的 sort 和 ep 时出错"
	LogMsgWarnChineseNumberNotFullySupported = "警告: 中文数字或复杂季度字符串 '%s' 未完全支持, 默认设置为 %d. "
)

const (
	DateFormat = "2006-01-02"
)

const (
	RegexPatternPunctuationsSpaces   = `[\p{P}\p{S}\s]`
	RegexPatternChineseSeason        = `[第\s]*([一二三四五六七八九十0-9]+)\s*(?:季|期)`
	RegexPatternEnglishSeason        = `Season\s*([0-9]+)`
	RegexPatternEnglishOrdinalSeason = `([0-9]{1,2})(?:st|nd|rd|th)\s+season`
)

var (
	bangumiClientInstance *BangumiAPIClient
	bangumiClientOnce     sync.Once
)

// GetBangumiAPIClient 获取 Bangumi API 客户端的单例实例。
func GetBangumiAPIClient() *BangumiAPIClient {
	bangumiClientOnce.Do(func() {
		bangumiClientInstance = NewBangumiAPIClient()
	})
	return bangumiClientInstance
}

// BangumiAPIClient 提供了与 Bangumi API 交互的方法。
type BangumiAPIClient struct {
	apiClient           *net.APIClient
	cache               *cache.Cache
	urls                map[string]string
	userAgent           string
	seasonNumberRegexes []*regexp.Regexp
}

// NewBangumiAPIClient 创建一个新的 BangumiAPIClient 实例。
func NewBangumiAPIClient() *BangumiAPIClient {
	return &BangumiAPIClient{
		apiClient: net.NewAPIClient(),
		cache:     cache.New(BangumiCacheTTL, BangumiCacheCleanupInterval),
		urls: map[string]string{
			"search":   BangumiAPISearchEndpoint,
			"detail":   BangumiAPIDetailEndpoint,
			"subjects": BangumiAPISubjectsEndpoint,
			"episodes": BangumiAPIEpisodesEndpoint,
		},
		userAgent: "wikrin/CureTMDbAnime (https://github.com/wikrin/CureTMDbAnime)",
		seasonNumberRegexes: []*regexp.Regexp{
			regexp.MustCompile(RegexPatternChineseSeason),
			regexp.MustCompile(RegexPatternEnglishSeason),
			regexp.MustCompile(RegexPatternEnglishOrdinalSeason),
		},
	}
}

// invoke 向 Bangumi API 发送请求。
func (b *BangumiAPIClient) invoke(method, endpoint string, body any, params url.Values) (any, error) {
	cacheKeyBuilder := strings.Builder{}
	cacheKeyBuilder.WriteString(method)
	cacheKeyBuilder.WriteString(":")
	cacheKeyBuilder.WriteString(BangumiAPIBaseURL)
	cacheKeyBuilder.WriteString(endpoint)
	if params != nil {
		cacheKeyBuilder.WriteString("?")
		cacheKeyBuilder.WriteString(params.Encode())
	}
	cacheKey := cacheKeyBuilder.String()

	if method == http.MethodGet {
		if cachedData, found := b.cache.Get(cacheKey); found {
			return cachedData, nil
		}
	}

	headers := make(map[string]string)
	headers["User-Agent"] = b.userAgent

	bodyBytes, err := b.apiClient.DoRequest(context.Background(), method, BangumiAPIBaseURL, endpoint, body, params, headers)
	if err != nil {
		return nil, fmt.Errorf(ErrMsgBangumiAPIRequestFailed+": %w", err)
	}

	result, err := net.UnmarshalResponse[any](bodyBytes)
	if err != nil {
		return nil, fmt.Errorf(ErrMsgJSONDecodeResponseFailed+": %w", err)
	}

	if method == http.MethodGet {
		b.cache.Set(cacheKey, result, cache.DefaultExpiration)
	}

	return result, nil
}

// Search 在 Bangumi 上搜索条目。
func (b *BangumiAPIClient) Search(title string, airDate *string) ([]map[string]any, error) {
	if title == "" || airDate == nil || *airDate == "" {
		return nil, nil
	}

	parsedAirDate, err := time.Parse(DateFormat, *airDate)
	if err != nil {
		logger.Error("%s: airDate=%s, 错误=%v", LogMsgParseAirDateFailed, *airDate, err)
		return nil, fmt.Errorf("搜索: 解析播出日期 '%s' 失败: %w", *airDate, err)
	}

	start := parsedAirDate.AddDate(0, 0, -SearchDateRangeOffset)
	end := parsedAirDate.AddDate(0, 0, SearchDateRangeOffset)

	re := regexp.MustCompile(RegexPatternPunctuationsSpaces)
	cleanedTitle := re.ReplaceAllString(title, "")

	jsonBody := map[string]any{
		BangumiSearchKeyword: cleanedTitle,
		BangumiSearchSort:    BangumiSearchSortMatch,
		BangumiSearchFilter: map[string]any{
			BangumiSearchType:    []int{BangumiSearchTypeAnime},
			BangumiSearchAirDate: []string{fmt.Sprintf(">=%s", start.Format(DateFormat)), fmt.Sprintf("<=%s", end.Format(DateFormat))},
		},
	}

	resp, err := b.invoke(http.MethodPost, b.urls["search"], jsonBody, nil)
	if err != nil {
		logger.Error("%s: %v", ErrMsgBangumiAPIRequestFailed, err)
		return nil, fmt.Errorf("搜索: 调用 Bangumi API 搜索 '%s' 失败: %w", title, err)
	}

	if resp == nil {
		return nil, nil
	}

	respMap, ok := resp.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(ErrMsgSearchResponseFormat, resp)
	}

	data, ok := respMap[BangumiResponseDataKey].([]any)
	if !ok {
		return nil, fmt.Errorf(ErrMsgSearchDataFieldFormat, respMap[BangumiResponseDataKey])
	}

	var results []map[string]any
	for _, item := range data {
		if m, isMap := item.(map[string]any); isMap {
			results = append(results, m)
		} else {
			return nil, fmt.Errorf(ErrMsgSearchDataItemFormat, item)
		}
	}
	return results, nil
}

// Detail 获取条目的详细信息。
func (b *BangumiAPIClient) Detail(bid int) (map[string]any, error) {
	endpoint := fmt.Sprintf(b.urls["detail"], bid)
	resp, err := b.invoke(http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("详情: 获取 Bangumi ID '%d' 详情失败: %w", bid, err)
	}
	if resp == nil {
		return nil, nil
	}

	detailMap, ok := resp.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(ErrMsgDetailResponseFormat, resp)
	}
	return detailMap, nil
}

// Subjects 获取相关条目。
func (b *BangumiAPIClient) Subjects(bid int) ([]map[string]any, error) {
	endpoint := fmt.Sprintf(b.urls["subjects"], bid)
	resp, err := b.invoke(http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("相关条目: 获取 Bangumi ID '%d' 相关条目失败: %w", bid, err)
	}
	if resp == nil {
		return nil, nil
	}

	var data []any
	var ok bool

	data, ok = resp.([]any)
	if !ok {
		respMap, isMap := resp.(map[string]any)
		if isMap {
			data, ok = respMap[""].([]any)
			if !ok {
				return nil, fmt.Errorf(ErrMsgSubjectsResponseFormat, resp)
			}
		} else {
			return nil, fmt.Errorf(ErrMsgSubjectsResponseFormat, resp)
		}
	}

	var results []map[string]any
	for _, item := range data {
		if m, isMap := item.(map[string]any); isMap {
			results = append(results, m)
		} else {
			return nil, fmt.Errorf(ErrMsgSubjectsDataItemFormat, item)
		}
	}
	return results, nil
}

// Episodes 获取条目的剧集列表。
func (b *BangumiAPIClient) Episodes(bid int) ([]map[string]any, error) {
	endpoint := fmt.Sprintf(b.urls["episodes"], bid)
	resp, err := b.invoke(http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("剧集: 获取 Bangumi ID '%d' 剧集列表失败: %w", bid, err)
	}
	if resp == nil {
		return nil, nil
	}

	respMap, ok := resp.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(ErrMsgEpisodesResponseFormat, resp)
	}

	data, ok := respMap[BangumiResponseDataKey].([]any)
	if !ok {
		return nil, fmt.Errorf(ErrMsgEpisodesDataFieldFormat, respMap[BangumiResponseDataKey])
	}

	var results []map[string]any
	for _, item := range data {
		if m, isMap := item.(map[string]any); isMap {
			results = append(results, m)
		} else {
			return nil, fmt.Errorf(ErrMsgEpisodesDataItemFormat, item)
		}
	}
	return results, nil
}

// GetAllSequels 递归获取所有续集条目 ID。
func (b *BangumiAPIClient) GetAllSequels(bid int) ([]int, error) {
	result := make(map[int]struct{})
	var sequence []int

	var recursiveFetch func(currentBID int)
	recursiveFetch = func(currentBID int) {
		if _, exists := result[currentBID]; exists {
			return
		}
		result[currentBID] = struct{}{}
		sequence = append(sequence, currentBID)

		related, err := b.Subjects(currentBID)
		if err != nil {
			logger.Error("%s: Bangumi ID=%d, 错误=%v", ErrMsgFetchSubjectsFailed, currentBID, err)
			return
		}
		if related == nil {
			return
		}
		for _, item := range related {
			if relation, ok := item[BangumiResponseRelation].(string); ok && relation == BangumiRelationSequel {
				if id, ok := item[BangumiResponseID].(float64); ok {
					recursiveFetch(int(id))
				}
			}
		}
	}

	recursiveFetch(bid)
	return sequence, nil
}

// SeasonInfo 处理 Bangumi 条目以创建 SeriesEntry。
func (b *BangumiAPIClient) SeasonInfo(item map[string]any) (*model.SeriesEntry, error) {
	if item == nil {
		return nil, nil
	}

	bgmSeasons := make(map[int]*model.SeasonEntry)

	sids, err := b.GetAllSequels(int(item[BangumiResponseID].(float64)))
	if err != nil {
		return nil, fmt.Errorf("季信息: 获取所有续集 ID 失败: %w", err)
	}

	for _, sid := range sids {
		detail, err := b.Detail(sid)
		if err != nil {
			logger.Error("%s: Bangumi ID=%d, 错误=%v", ErrMsgGetDetailFailed, sid, err)
			continue
		}
		if detail == nil || (detail[BangumiPlatform] != nil && detail[BangumiPlatform].(string) == BangumiPlatformMovie) {
			continue
		}

		nameCN := ""
		if nc, ok := detail[BangumiNameCN].(string); ok {
			nameCN = nc
		}
		name := ""
		if n, ok := detail[BangumiName].(string); ok {
			name = n
		}

		num := b.ExtractSeasonNumber(name, nameCN)

		eps := 0
		if detail[BangumiEpisodes] != nil {
			eps = int(detail[BangumiEpisodes].(float64))
		} else if detail[BangumiTotalEpisodes] != nil {
			eps = int(detail[BangumiTotalEpisodes].(float64))
		} else {
			eps = DefaultEpisodeCount
		}

		if _, ok := bgmSeasons[num]; !ok {
			bgmSeasons[num] = &model.SeasonEntry{
				EpisodeCount: eps,
				Name:         &nameCN,
				SeasonNumber: num,
			}
		} else {
			sortVal, epVal, err := b.GetSortAndEp(sid)
			if err != nil {
				logger.Error("%s: Bangumi ID=%d, 错误=%v", ErrMsgGetSortAndEpFailed, sid, err)
				bgmSeasons[num].EpisodeCount += eps
				continue
			}

			if sortVal != nil && epVal != nil && *sortVal == *epVal {
				maxSeasonNum := 0
				for k := range bgmSeasons {
					if k > maxSeasonNum {
						maxSeasonNum = k
					}
				}
				newNum := maxSeasonNum + 1
				bgmSeasons[newNum] = &model.SeasonEntry{
					EpisodeCount: eps,
					Name:         &nameCN,
					SeasonNumber: newNum,
				}
			} else {
				bgmSeasons[num].EpisodeCount += eps
			}
		}
	}

	var seasons []*model.SeasonEntry
	for _, s := range bgmSeasons {
		seasons = append(seasons, s)
	}
	sort.Slice(seasons, func(i, j int) bool {
		return seasons[i].SeasonNumber < seasons[j].SeasonNumber
	})

	return &model.SeriesEntry{Seasons: seasons}, nil
}

// GetSortAndEp 从剧集中获取 sort 和 ep 值。
func (b *BangumiAPIClient) GetSortAndEp(sid int) (*int, *int, error) {
	episodes, err := b.Episodes(sid)
	if err != nil {
		return nil, nil, fmt.Errorf("获取排序与剧集号: 获取 Bangumi ID '%d' 剧集失败: %w", sid, err)
	}
	if len(episodes) == 0 {
		return nil, nil, nil
	}

	ep0 := episodes[0]
	var sortVal, epVal *int

	if sortData, ok := ep0[BangumiSortKey].(float64); ok {
		s := int(sortData)
		sortVal = &s
	}
	if epData, ok := ep0[BangumiEpKey].(float64); ok {
		e := int(epData)
		epVal = &e
	}

	if sortVal == nil || epVal == nil {
		return nil, nil, nil
	}
	return sortVal, epVal, nil
}

// ExtractSeasonNumber 从名称中提取季度号。
func (b *BangumiAPIClient) ExtractSeasonNumber(name, nameCN string) int {
	parse := func(text string) int {
		if text == "" {
			return 0
		}
		patterns := b.seasonNumberRegexes
		for _, re := range patterns {
			m := re.FindStringSubmatch(text)
			if len(m) > 1 {
				if num, err := strconv.Atoi(m[1]); err == nil {
					return num
				}
				switch m[1] {
				case "一":
					return 1
				case "二":
					return 2
				case "三":
					return 3
				case "四":
					return 4
				case "五":
					return 5
				case "六":
					return 6
				case "七":
					return 7
				case "八":
					return 8
				case "九":
					return 9
				case "十":
					return 10
				case "Ⅰ":
					return 1
				case "Ⅱ":
					return 2
				case "Ⅲ":
					return 3
				case "Ⅳ":
					return 4
				default:
					logger.Warn("%s: 数字=%s, 默认季度号=%d", LogMsgWarnChineseNumberNotFullySupported, m[1], DefaultSeasonNumber)
					return DefaultSeasonNumber
				}
			}
		}
		return 0
	}

	if num := parse(nameCN); num != 0 {
		return num
	}
	if num := parse(name); num != 0 {
		return num
	}
	return DefaultSeasonNumber
}
