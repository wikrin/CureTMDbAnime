package model

import (
	"fmt"
	"sort"

	"curetmdbanime/internal/collection"
)

// IncludeEntry 表示一个包含条目
type IncludeEntry struct {
	Order         int    `json:"order"`                    // 集
	EpisodeNumber *int   `json:"episode_number,omitempty"` // 原集号
	SeasonNumber  *int   `json:"season_number,omitempty"`  // 原季号
	Count         int    `json:"count"`                    // 数量
	Type          string `json:"type"`                     // 类型
	TMDBID        *int   `json:"tmdbid,omitempty"`         // TMDB ID
}

func (ie *IncludeEntry) ApplyDefaults() {
	// 默认为 1
	if ie.Count == 0 {
		ie.Count = 1
	}
	// 确定类型
	if ie.Type == "" {
		if ie.EpisodeNumber == nil && ie.SeasonNumber == nil && ie.TMDBID != nil {
			ie.Type = "movie"
		} else {
			ie.Type = "tv"
		}
	}
}

func (ie *IncludeEntry) IsSpecialSeason() bool {
	if ie.Type == "tv" && ie.EpisodeNumber != nil && *ie.SeasonNumber == 0 {
		return true
	}
	return false
}

// 季的条目信息
type SeasonEntry struct {
	Name         *string         `json:"name,omitempty"`    // 季名称
	EpisodeCount int             `json:"episode_count"`     // 剧集总数
	SeasonNumber int             `json:"season_number"`     // 季编号
	Include      []*IncludeEntry `json:"include,omitempty"` // 包含的剧集范围
}

// HasSpecialSeason 检查该季条目的 Include 列表中是否包含特殊季（第0季）的条目
func (se *SeasonEntry) HasSpecialSeason() bool {
	if se.Include == nil {
		return false
	}

	for _, include := range se.Include {
		if include.IsSpecialSeason() {
			return true
		}
	}

	return false
}

// 检查实际剧集数量是否有效
// 参数 seasons: 一个 map, 键为季编号, 值为剧集列表
// 返回值: 如果实际剧集数量小于或等于 SeasonEntry 中定义的剧集总数, 则返回 true；
// 如果没有找到该季的剧集, 也视为有效（true）
func (se *SeasonEntry) EpisodeCountValid(seasons map[int][]int) bool {
	if actualEpisodes, ok := seasons[se.SeasonNumber]; ok {
		return len(actualEpisodes) <= se.EpisodeCount
	}
	return true // 未找到该季的剧集, 认为是有效的
}

// 返回一个包含剧集信息的映射
// 返回值: 一个 map, 键为剧集顺序, 值为 EpisodeMap 指针, 表示包含的剧集
func (se *SeasonEntry) IncludedEpisodes() map[int]*EpisodeMap {
	result := make(map[int]*EpisodeMap)
	if se.Include == nil {
		return result
	}
	for _, item := range se.Include {
		for e := range item.Count {
			order := e + item.Order
			epNum := e
			if item.EpisodeNumber != nil {
				epNum += *item.EpisodeNumber
			}
			result[order] = &EpisodeMap{
				Order:         order,
				EpisodeNumber: &epNum,
				SeasonNumber:  item.SeasonNumber,
				Type:          item.Type,
				TMDBID:        item.TMDBID,
			}
		}
	}
	return result
}

// 返回一个已排序的缺失剧集编号列表
// 该函数通过比较 `Include` 中定义的剧集和 `EpisodeCount` 定义的总剧集数来找出缺失的剧集
// 返回值: 一个整数切片, 包含所有缺失的剧集编号
func (se *SeasonEntry) MissingEpisodes() []int {
	// current 存储 SeasonEntry 中 Include 字段包含的所有剧集顺序
	current := collection.NewSet[int]()
	for _, item := range se.Include {
		for order := item.Order; order < item.Order+item.Count; order++ {
			current.Add(order)
		}
	}

	// allExpected 存储所有期望的剧集顺序（从1到EpisodeCount）
	allExpected := collection.NewSet[int]()
	for i := 1; i <= se.EpisodeCount; i++ {
		allExpected.Add(i)
	}

	// extra 存储在 current 中存在但不在 allExpected 中的剧集（即多余的剧集）
	extra := current.Difference(allExpected)

	// missing 存储在 allExpected 中存在但不在 current 中的剧集（即缺失的剧集）
	missing := allExpected.Difference(current).ToSlice()
	sort.Ints(missing) // 对缺失剧集进行排序

	// 如果有多余的剧集
	if len(extra) > 0 {
		// 如果缺失的剧集数量多于多余的剧集数量, 则从缺失剧集列表中移除与多余剧集数量相同的部分
		if len(missing) > len(extra) {
			return missing[:len(missing)-len(extra)]
		}
		// 如果多余的剧集数量大于或等于缺失剧集数量, 则认为没有缺失剧集（经过调整后）
		return []int{} // 多余的剧集数量多于或等于缺失剧集数量, 因此调整后没有缺失剧集
	}
	return missing // 没有多余剧集, 直接返回缺失剧集列表
}

// SeriesEntry 表示一个系列条目信息
type SeriesEntry struct {
	Name    *string        `json:"name,omitempty"` // 系列名称
	Seasons []*SeasonEntry `json:"seasons"`        // 包含的季列表
}

func (t *SeriesEntry) Decode(data map[string]any) error {
	err := decodeWithMapstructure(data, t)
	if err != nil {
		return err
	}
	// 为 IncludeEntry 应用默认值
	for _, season := range t.Seasons {
		for _, item := range season.Include {
			item.ApplyDefaults()
		}
	}
	return nil
}

// HasAnySpecialSeason 检查该系列中任意季的 Include 列表是否包含特殊季（第0季）的条目
func (se *SeriesEntry) HasAnySpecialSeason() bool {
	if se.Seasons == nil {
		return false
	}

	for _, season := range se.Seasons {
		if season.HasSpecialSeason() {
			return true
		}
	}

	return false
}

// 根据季编号返回特定的季条目
// 参数 num: 要查找的季编号
// 返回值: 对应的 SeasonEntry 指针；如果未找到, 则返回 nil
func (se *SeriesEntry) Season(num int) *SeasonEntry {
	for _, s := range se.Seasons {
		if s.SeasonNumber == num {
			return s
		}
	}
	return nil
}

// 检查是否有任何季的剧集数量不一致
// 参数 seasons: 一个 map, 键为季编号, 值为剧集列表
// 返回值: 如果存在任何季的剧集数量不一致, 则返回 true；否则返回 false
func (se *SeriesEntry) HasInconsistent(seasons map[int][]int) bool {
	for _, season := range se.Seasons {
		if !season.EpisodeCountValid(seasons) {
			return true
		}
	}
	return false
}

// 返回一个从季编号到季名称的映射
// 返回值: 一个 map, 键为季编号, 值为季名称 (string)
func (se *SeriesEntry) SeasonNames() map[int]string {
	names := make(map[int]string)
	for _, season := range se.Seasons {
		if season.Name != nil {
			names[season.SeasonNumber] = *season.Name
		} else {
			names[season.SeasonNumber] = fmt.Sprintf("第 %d 季", season.SeasonNumber)
		}
	}
	return names
}

// 返回一个按季编号分类的缺失剧集映射
// 返回值: 一个 map, 键为季编号, 值为该季缺失剧集编号的切片
func (se *SeriesEntry) MissingEpisodesBySeason() map[int][]int {
	missing := make(map[int][]int)
	for _, season := range se.Seasons {
		missing[season.SeasonNumber] = season.MissingEpisodes()
	}
	return missing
}

// 返回一个从季编号到剧集映射的映射
// 返回值: 一个 map, 键为季编号, 值为该季的剧集映射 (map[int]*EpisodeMap)
func (se *SeriesEntry) EpisodeMapping() map[int]map[int]*EpisodeMap {
	mapping := make(map[int]map[int]*EpisodeMap)
	for _, season := range se.Seasons {
		if len(season.IncludedEpisodes()) > 0 {
			mapping[season.SeasonNumber] = season.IncludedEpisodes()
		}
	}
	return mapping
}

// 返回系列中最小的季编号
// 返回值: 最小的季编号 如果系列没有季, 则返回 1
func (se *SeriesEntry) MinSeason() int {
	if len(se.Seasons) == 0 {
		return 1 // 如果没有季, 默认返回第一季
	}
	min := se.Seasons[0].SeasonNumber
	for _, s := range se.Seasons {
		if s.SeasonNumber < min {
			min = s.SeasonNumber
		}
	}
	return min
}
