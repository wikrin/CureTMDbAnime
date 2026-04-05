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
	BangumiPlatformTV       = "TV"
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
	DateFormat = "2006-01-02"
)

const (
	RegexPatternPunctuationsSpaces   = `[\p{P}\p{S}\s]`
	RegexPatternChineseSeason        = `[第\s]*([一二三四五六七八九十壹贰叁肆伍陆柒捌玖拾0-9]+)\s*(?:季|期)`
	RegexPatternEnglishSeason        = `(?:Season|S)\s*([0-9]+)`
	RegexPatternEnglishOrdinalSeason = `([0-9]{1,2})(?:st|nd|rd|th)\s+season`
	RegexPatternRomanSeason          = `(?i)(?:Season|S)\s*([IVXLCDM]+)`
)

// numberConverter 将各种数字字符串转换为整数。
func numberConverter(s string) int {
	if num, err := strconv.Atoi(s); err == nil {
		return num
	}

	chineseNumberMap := map[rune]int{
		'零': 0, '一': 1, '二': 2, '三': 3, '四': 4,
		'五': 5, '六': 6, '七': 7, '八': 8, '九': 9,
		'壹': 1, '贰': 2, '叁': 3, '肆': 4, '伍': 5,
		'陆': 6, '柒': 7, '捌': 8, '玖': 9,
	}
	chineseUnitMap := map[rune]int{
		'十': 10, '拾': 10,
	}

	// 尝试解析中文数字
	// 扩展正则表达式以匹配“几十几”的模式，例如“二十三”
	if isChinese := regexp.MustCompile(`^[零一二三四五六七八九十壹贰叁肆伍陆柒捌玖拾]+$`).MatchString(s); isChinese {
		runes := []rune(s)
		result := 0
		temp := 0 // 存储当前数字值

		for i := range runes {
			r := runes[i]
			if val, ok := chineseNumberMap[r]; ok {
				temp = val
			} else if unit, ok := chineseUnitMap[r]; ok {
				if temp == 0 { // 处理“十”或“拾”单独出现的情况，例如“十一”中的“十”
					temp = 1
				}
				result += temp * unit
				temp = 0 // 重置temp，以便处理下一个数字
			} else {
				return 0 // 遇到无法识别的中文数字字符
			}
		}
		result += temp // 加上最后一个数字（例如“二十三”中的“三”）

		if result != 0 {
			return result
		}
	}

	romanNumerals := map[rune]int{
		'I': 1,
		'V': 5,
		'X': 10,
		'L': 50,
		'C': 100,
		'D': 500,
		'M': 1000,
	}

	// 尝试解析罗马数字
	sUpper := strings.ToUpper(s)
	// 使用 RegexPatternRomanSeason 的字符集来判断是否为罗马数字
	if isRoman := regexp.MustCompile(`^[IVXLCDM]+$`).MatchString(sUpper); isRoman {
		total := 0
		prev := 0
		for i := len(sUpper) - 1; i >= 0; i-- {
			curr := romanNumerals[rune(sUpper[i])]
			if curr == 0 { // 非法罗马数字字符
				return 0
			}
			if curr < prev {
				total -= curr
			} else {
				total += curr
			}
			prev = curr
		}
		if total != 0 { // 只有在有效解析到罗马数字时才返回
			return total
		}
	}

	return 0
}

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
			regexp.MustCompile(RegexPatternRomanSeason),
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
		return nil, fmt.Errorf("Bangumi API 请求错误: %w", err)
	}

	result, err := net.UnmarshalResponse[any](bodyBytes)
	if err != nil {
		return nil, fmt.Errorf("JSON 响应体解码错误: %w", err)
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
		logger.Error("解析播出日期 %s 错误: %v", *airDate, err)
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
		logger.Error("Bangumi API 请求错误: %v", err)
		return nil, fmt.Errorf("搜索: 调用 Bangumi API 搜索 '%s' 失败: %w", title, err)
	}

	if resp == nil {
		return nil, nil
	}

	respMap, ok := resp.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("搜索响应格式错误: 预期为 map[string]any, 实际为 %T", resp)
	}

	data, ok := respMap[BangumiResponseDataKey].([]any)
	if !ok {
		return nil, fmt.Errorf("搜索响应中 'data' 字段格式错误: 预期为 []any, 实际为 %T", respMap[BangumiResponseDataKey])
	}

	var results []map[string]any
	for _, item := range data {
		if m, isMap := item.(map[string]any); isMap {
			results = append(results, m)
		} else {
			return nil, fmt.Errorf("搜索响应中的数据项格式错误: 预期为 map[string]any, 实际为 %T", item)
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
		return nil, fmt.Errorf("详情响应格式错误: 预期为 map[string]any, 实际为 %T", resp)
	}
	return detailMap, nil
}

// Subjects 获取相关条目。
func (b *BangumiAPIClient) Subjects(bid int) ([]map[string]any, error) {
	endpoint := fmt.Sprintf(b.urls["subjects"], bid)
	resp, err := b.invoke(http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("获取 Bangumi ID '%d' 相关条目失败: %w", bid, err)
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
				return nil, fmt.Errorf("相关条目响应格式错误: 预期为 []any 或 {\"\": []any}, 实际为 %T", resp)
			}
		} else {
			return nil, fmt.Errorf("相关条目响应格式错误: 预期为 []any 或 {\"\": []any}, 实际为 %T", resp)
		}
	}

	var results []map[string]any
	for _, item := range data {
		if m, isMap := item.(map[string]any); isMap {
			results = append(results, m)
		} else {
			return nil, fmt.Errorf("相关条目数据项格式错误: 预期为 map[string]any, 实际为 %T", item)
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
		return nil, fmt.Errorf("剧集列表响应格式错误: 预期为 map[string]any, 实际为 %T", resp)
	}

	data, ok := respMap[BangumiResponseDataKey].([]any)
	if !ok {
		return nil, fmt.Errorf("剧集列表响应中 'data' 字段格式错误: 预期为 []any, 实际为 %T", respMap[BangumiResponseDataKey])
	}

	var results []map[string]any
	for _, item := range data {
		if m, isMap := item.(map[string]any); isMap {
			results = append(results, m)
		} else {
			return nil, fmt.Errorf("剧集列表数据项格式错误: 预期为 map[string]any, 实际为 %T", item)
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
			logger.Error("错误=%v", err)
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
	logger.Info("Bangumi ID %d 续集条目: %d", bid, sequence)
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

	// Bangumi系列条目仅一个时，返回 nil 不进行拆分处理
	if len(sids) < 2 {
		return nil, nil
	}

	for _, sid := range sids {
		detail, err := b.Detail(sid)
		if err != nil {
			logger.Error("获取 Bangumi ID %d 的详情时出错: 错误=%v", sid, err)
			continue
		}else if detail == nil {
			logger.Warn("Bangumi ID %d 详情为空", sid)
			continue
		}else if detail[BangumiPlatform] != nil && detail[BangumiPlatform].(string) != BangumiPlatformTV {
			logger.Info("Bangumi ID %d, %s 不符合 %s, 跳过处理", sid, detail[BangumiPlatform], BangumiPlatformTV)
			continue
		}

		logger.Debug("处理条目[%d]: %s",sid, detail)
		nameCN := ""
		if nc, ok := detail[BangumiNameCN].(string); ok {
			nameCN = nc
		}
		name := ""
		if n, ok := detail[BangumiName].(string); ok {
			name = n
		}

		num := b.ExtractSeasonNumber(name, nameCN)

		eps := DefaultEpisodeCount
		if val, ok := detail[BangumiEpisodes].(float64); ok && val > 0 {
			eps = int(val)
		} else if val, ok := detail[BangumiTotalEpisodes].(float64); ok && val > 0 {
			eps = int(val)
		}

		if _, ok := bgmSeasons[num]; !ok {
			bgmSeasons[num] = &model.SeasonEntry{
				EpisodeCount: eps,
				Name:         &nameCN,
				SeasonNumber: num,
			}
		} else {
			isSortEqualEp, err := b.IsFirstEpisodeSequential(sid)
			if err != nil {
				logger.Error("错误=%v", err)
				bgmSeasons[num].EpisodeCount += eps
				continue
			}

			if isSortEqualEp {
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

// IsFirstEpisodeSequential 判断首集的 sort 和 ep 值是否相等。
func (b *BangumiAPIClient) IsFirstEpisodeSequential(sid int) (bool, error) {
	episodes, err := b.Episodes(sid)
	if err != nil {
		return false, fmt.Errorf("Bangumi ID '%d' 比较顺序号序与剧集号失败: 获取剧集失败: %w", sid, err)
	}
	if len(episodes) == 0 {
		return false, fmt.Errorf("Bangumi ID '%d' 比较顺序号序与剧集号失败: 剧集列表为空", sid)
	}

	ep0 := episodes[0]
	sortData, sortOK := ep0[BangumiSortKey].(float64)
	epData, epOK := ep0[BangumiEpKey].(float64)
	if !sortOK || !epOK {
		return false, nil
	}

	return int(sortData) == int(epData), nil
}

// ExtractSeasonNumber 从名称中提取季度号。
func (b *BangumiAPIClient) ExtractSeasonNumber(name, nameCN string) int {
	parse := func(text string) int {
		if text == "" {
			return 0 // 未找到时返回 0
		}
		patterns := b.seasonNumberRegexes
		for _, re := range patterns {
			m := re.FindStringSubmatch(text)
			if len(m) > 1 {
				if num := numberConverter(m[1]); num != 0 {
					return num
				}
				logger.Warn("无法识别的季号字符串 '%s', 默认设置为 %d.", m[1], DefaultSeasonNumber)
				return DefaultSeasonNumber
			}
		}
		return 0
	}

	// 优先从 nameCN 提取
	if seasonNum := parse(nameCN); seasonNum != 0 {
		return seasonNum
	}

	// 如果 nameCN 没找到，尝试从 name 提取
	if seasonNum := parse(name); seasonNum != 0 {
		return seasonNum
	}

	// 如果两个都未能提取到有效季号，则返回 DefaultSeasonNumber (1)
	return DefaultSeasonNumber
}
