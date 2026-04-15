package model

import (
	"fmt"
	"maps"
	"sort"

	"curetmdbanime/internal/collection"
)

// 业务逻辑处理后的系列数据结构
type LogicSeries struct {
	Name        *string        `json:"name,omitempty"`        // 系列名称
	Seasons     []*LogicSeason `json:"seasons"`               // 逻辑季列表
	VoteAverage float64        `json:"vote_average"`          // 平均评分
	PosterPath  *string        `json:"poster_path,omitempty"` // 海报路径
	Overview    *string        `json:"overview,omitempty"`    // 概述
}

// orgMap 聚合所有季的剧集映射
func (ls *LogicSeries) OrgMap() map[IntPair]IntPair {
	result := make(map[IntPair]IntPair)

	for _, season := range ls.Seasons {
		seasonMap := season.OrgMap()
		maps.Copy(result, seasonMap)
	}

	return result
}

// 向 LogicSeries 添加一个 LogicSeason
func (ls *LogicSeries) AddSeason(name *string, airDate *string,
	seasonNumber int, episodesMap map[int]*EpisodeMap, kwargs map[string]any) {
	newSeason := &LogicSeason{
		Name:         name,
		AirDate:      airDate,
		SeasonNumber: seasonNumber,
		EpisodesMap:  episodesMap,
	}

	// 设置季的平均评分，若无则使用系列的
	if va, ok := kwargs["vote_average"].(float64); ok {
		newSeason.VoteAverage = &va
	} else {
		newSeason.VoteAverage = &ls.VoteAverage
	}
	// 设置季的海报路径，若无则使用系列的
	if pp, ok := kwargs["poster_path"].(string); ok {
		newSeason.PosterPath = &pp
	} else {
		newSeason.PosterPath = ls.PosterPath
	}
	// 设置季的概述，若无则使用系列的
	if ov, ok := kwargs["overview"].(string); ok {
		newSeason.Overview = &ov
	} else {
		newSeason.Overview = ls.Overview
	}

	ls.Seasons = append(ls.Seasons, newSeason) // 添加新季
}

// 根据季编号返回 LogicSeason
func (ls *LogicSeries) SeasonInfo(seasonNumber int) *LogicSeason {
	for _, season := range ls.Seasons {
		if season.SeasonNumber == seasonNumber {
			return season
		}
	}
	return nil
}

// 业务逻辑处理后的季数据结构
type LogicSeason struct {
	Name         *string             `json:"name,omitempty"`         // 季名称
	AirDate      *string             `json:"air_date,omitempty"`     // 上映日期
	SeasonNumber int                 `json:"season_number"`          // 季编号
	EpisodesMap  map[int]*EpisodeMap `json:"episodes_map"`           // 剧集映射 (键为剧集顺序)
	VoteAverage  *float64            `json:"vote_average,omitempty"` // 平均评分
	PosterPath   *string             `json:"poster_path,omitempty"`  // 海报路径
	Overview     *string             `json:"overview,omitempty"`     // 概述
}

// 返回该季包含的剧集数量
func (ls *LogicSeason) EpisodeCount() *int {
	count := len(ls.EpisodesMap)
	return &count
}

// OrgMap 返回原始剧集到新剧集的映射
// - 电视剧: (tmdb_季号, tmdb_集号) -> 当前季号, 当前集号
// - 电影: (-1, tmdbid) -> 当前季号, 当前集号
func (ls *LogicSeason) OrgMap() map[IntPair]IntPair {
	offset := 0
	// 连续集号暂时搁置实现
	// if useContEps {
	//     for ep, org := range ls.EpisodesMap {
	//         if *org.SeasonNumber != 0 && org.EpisodeNumber != nil {
	//             offset = *org.EpisodeNumber - ep
	//             break
	//         }
	//     }
	// }

	result := make(map[IntPair]IntPair, len(ls.EpisodesMap))
	for ep, org := range ls.EpisodesMap {
		result[org.MappingKey()] = IntPair{ls.SeasonNumber, ep + offset}
	}
	return result
}

// 返回唯一的 EpisodeMap 列表 (基于 season_number, type, tmdbid 去重)
func (ls *LogicSeason) UniqueEntry() []*EpisodeMap {
	if len(ls.EpisodesMap) == 0 {
		return []*EpisodeMap{}
	}

	seen := collection.NewSet[string]() // 记录已见过的唯一键
	result := []*EpisodeMap{}

	for _, mapping := range ls.EpisodesMap {
		// 提取关键字段判断唯一性
		seasonNumStr := ""
		if mapping.SeasonNumber != nil {
			seasonNumStr = fmt.Sprintf("%d", *mapping.SeasonNumber)
		}

		tmdbIDStr := ""
		if mapping.TMDBID != nil {
			tmdbIDStr = fmt.Sprintf("%d", *mapping.TMDBID)
		}

		key := fmt.Sprintf("%s_%s_%s", seasonNumStr, mapping.Type, tmdbIDStr)

		if exists := seen.Contains(key); !exists {
			seen.Add(key)
			result = append(result, mapping)
		}
	}

	return result
}

// 返回一个已排序的原始季编号列表
func (ls *LogicSeason) OrgSeasons() []int {
	seen := collection.NewSet[int]() // 记录已见过的季编号
	var seasons []int                // 存储提取到的季编号
	for _, epMap := range ls.EpisodesMap {
		if epMap.SeasonNumber != nil {
			// 如果该季编号未被记录, 则添加
			if ok := seen.Contains(*epMap.SeasonNumber); !ok {
				seen.Add(*epMap.SeasonNumber)
				seasons = append(seasons, *epMap.SeasonNumber)
			}
		}
	}
	sort.Ints(seasons) // 排序季编号
	return seasons
}

// 剧集映射信息
type EpisodeMap struct {
	Order         int    `json:"order"`                    // 剧集顺序
	EpisodeNumber *int   `json:"episode_number,omitempty"` // 原始集号
	SeasonNumber  *int   `json:"season_number,omitempty"`  // 原始季号
	Type          string `json:"type"`                     // 类型
	TMDBID        *int   `json:"tmdbid,omitempty"`         // TMDB ID
}

// KeyPart1 返回映射的第一部分键 (通常是季号)
func (em *EpisodeMap) SeasonNum() int {
	if em.SeasonNumber != nil {
		return *em.SeasonNumber
	}
	return -1
}

// KeyPart2 返回映射的第二部分键 (通常是集号或 TMDB ID)
func (em *EpisodeMap) EpisodeNum() int {
	if em.Type == "tv" || *em.EpisodeNumber > 0 {
		return *em.EpisodeNumber
	}
	if em.TMDBID != nil {
		return *em.TMDBID
	}
	return 0 // 默认值
}

// 返回 EpisodeMap 的唯一映射键
func (em *EpisodeMap) MappingKey() IntPair {
	return IntPair{em.SeasonNum(), em.EpisodeNum()}
}
