package model

// 它定义了剧集的详细属性, 用于在应用程序中表示和传输单个剧集数据
type Episode struct {
	ID            int            `json:"id" mapstructure:"id"`                                     // 唯一标识符
	Name          *string        `json:"name,omitempty" mapstructure:"name,omitempty"`             // 名称
	Overview      *string        `json:"overview,omitempty" mapstructure:"overview,omitempty"`     // 概要描述
	AirDate       *string        `json:"air_date,omitempty" mapstructure:"air_date,omitempty"`     // 首播日期
	EpisodeNumber int            `json:"episode_number" mapstructure:"episode_number"`             // 集号
	SeasonNumber  int            `json:"season_number" mapstructure:"season_number"`               // 季号
	StillPath     *string        `json:"still_path,omitempty" mapstructure:"still_path,omitempty"` // 剧集静态图片（如剧照）的相对路径
	Runtime       *int           `json:"runtime,omitempty" mapstructure:"runtime,omitempty"`       // 时长, 单位为分钟
	Other         map[string]any `mapstructure:",remain"`                                          // 额外字段
}

func (e *Episode) keyPart1() int {
	return e.SeasonNumber
}

func (e *Episode) keyPart2() int {
	return e.EpisodeNumber
}

// 返回 Episode 的 MappingKey 等效值
func (e *Episode) MappingKey() IntPair {
	return IntPair{e.keyPart1(), e.keyPart2()}
}

func (e Episode) MarshalJSON() ([]byte, error) { // 修改接收器类型
	// 定义一个别名类型以避免无限递归
	type Alias Episode // 修改别名类型
	// 将当前实例转换为别名类型
	temp := (Alias)(e)

	// 调用辅助函数合并已知字段和额外数据，并进行最终编码
	return mergeAndMarshal(temp, e.Other)
}

func (e *Episode) Decode(data map[string]any) error {
	err := decodeWithMapstructure(data, e)
	if err != nil {
		return err
	}
	return nil
}

// Season 结构体对应于外部服务（如 TMDb API）中的剧集季信息
// 它定义了剧集季的详细属性, 包括该季包含的剧集列表
type Season struct {
	ID           int            `json:"id" mapstructure:"id"`                                           // 唯一标识符
	Name         *string        `json:"name,omitempty" mapstructure:"name,omitempty"`                   // 季名称
	Overview     *string        `json:"overview,omitempty" mapstructure:"overview,omitempty"`           // 概要描述
	PosterPath   *string        `json:"poster_path,omitempty" mapstructure:"poster_path,omitempty"`     // 季海报
	SeasonNumber int            `json:"season_number" mapstructure:"season_number"`                     // 季号
	VoteAverage  *float64       `json:"vote_average,omitempty" mapstructure:"vote_average,omitempty"`   // 评分
	AirDate      *string        `json:"air_date,omitempty" mapstructure:"air_date,omitempty"`           // 季的首播日期
	EpisodeCount *int           `json:"episode_count,omitempty" mapstructure:"episode_count,omitempty"` // 季总集数
	Episodes     []Episode      `json:"episodes,omitempty" mapstructure:"episodes,omitempty"`           // 季包含的剧集列表
	Other        map[string]any `mapstructure:",remain"`                                                // 额外字段
}

func (s *Season) Decode(data map[string]any) error {
	err := decodeWithMapstructure(data, s)
	if err != nil {
		return err
	}
	return nil
}

func (s Season) MarshalJSON() ([]byte, error) { // 修改接收器类型
	// 定义一个别名类型以避免无限递归
	type Alias Season // 修改别名类型
	// 将当前实例转换为别名类型
	temp := (Alias)(s)

	// 调用辅助函数合并已知字段和额外数据，并进行最终编码
	return mergeAndMarshal(temp, s.Other)
}

// TMDb API 中的电视节目信息
type TVShow struct {
	ID               int              `json:"id" mapstructure:"id"`                                             // 唯一标识符
	Name             *string          `json:"name,omitempty" mapstructure:"name,omitempty"`                     // 节目名称
	OriginalName     *string          `json:"original_name,omitempty" mapstructure:"original_name,omitempty"`   // 节目原始名称
	Overview         *string          `json:"overview,omitempty" mapstructure:"overview,omitempty"`             // 概要描述
	PosterPath       *string          `json:"poster_path,omitempty" mapstructure:"poster_path,omitempty"`       // 海报图片
	BackdropPath     *string          `json:"backdrop_path,omitempty" mapstructure:"backdrop_path,omitempty"`   // 背景图片
	Seasons          []Season         `json:"seasons" mapstructure:"seasons"`                                   // 节目包含的所有季的列表
	NumberOfSeasons  int              `json:"number_of_seasons" mapstructure:"number_of_seasons"`               // 目包含的总季数
	NumberOfEpisodes int              `json:"number_of_episodes" mapstructure:"number_of_episodes"`             // 节目包含的总集数
	VoteAverage      *float64         `json:"vote_average,omitempty" mapstructure:"vote_average,omitempty"`     // 评分
	FirstAirDate     *string          `json:"first_air_date,omitempty" mapstructure:"first_air_date,omitempty"` // 节目的首播日期
	Genres           []map[string]any `json:"genres,omitempty" mapstructure:"genres,omitempty"`                 // 类型列表
	OriginCountry    []string         `json:"origin_country,omitempty" mapstructure:"origin_country,omitempty"` // 原产国
	Other            map[string]any   `mapstructure:",remain"`                                                  // 额外字段
}

func (t *TVShow) SeasonsInfo() map[int][]int {
	origSeasonsMap := make(map[int][]int, len(t.Seasons))
	for _, item := range t.Seasons {
		// 使用循环创建并填充从1开始的序列
		episodeSlice := make([]int, int(*item.EpisodeCount))
		for i := range episodeSlice {
			episodeSlice[i] = i + 1
		}
		origSeasonsMap[item.SeasonNumber] = episodeSlice
	}
	return origSeasonsMap
}

func (t *TVShow) FindAirdateBySeasonNumber(seasonNumber int) *string {
	for _, item := range t.Seasons {
		if item.SeasonNumber == seasonNumber {
			return item.AirDate
		}
	}
	return nil
}

func (t *TVShow) GenreIds() []int {
	ids := make([]int, len(t.Genres))
	for i, item := range t.Genres {
		if id, idOk := item["id"].(float64); idOk {
			ids[i] = int(id)
		}
	}
	return ids
}

func (t *TVShow) Decode(data map[string]any) error {
	err := decodeWithMapstructure(data, t)
	if err != nil {
		return err
	}
	return nil
}

func (t TVShow) MarshalJSON() ([]byte, error) { // 修改接收器类型
	// 定义一个别名类型以避免无限递归
	type Alias TVShow
	// 将当前实例转换为别名类型
	temp := (Alias)(t)

	// 调用辅助函数合并已知字段和额外数据，并进行最终编码
	return mergeAndMarshal(temp, t.Other)
}

func (t *TVShow) UpdateSeasonStats() {
	count := 0
	for _, season := range t.Seasons {
		if season.SeasonNumber != 0 { // 不计算第0季
			count++
		}
	}
	t.NumberOfSeasons = count

	total := 0
	for _, season := range t.Seasons {
		if season.SeasonNumber != 0 && season.EpisodeCount != nil { // 不计算第0季
			total += *season.EpisodeCount
		}
	}
	t.NumberOfEpisodes = total
}

// 当 Seasons 被修改时, 提供一个方法来更新统计
func (t *TVShow) SetSeasons(seasons []Season) {
	t.Seasons = seasons
	t.UpdateSeasonStats()
}
