package service_test

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"curetmdbanime/internal/config"
	"curetmdbanime/internal/service"
)

var (
	globalTVService *service.TVService
	globalAPIParams url.Values
)

// 用于在所有测试运行前加载配置文件、API 密钥并初始化全局服务
func TestMain(m *testing.M) {
	// 加载配置文件
	config.LoadConfig(nil)

	apiKey := os.Getenv("TMDB_API_KEY")
	if apiKey == "" {
		// 如果未设置 API 密钥，则所有集成测试将被跳过
		// 在 CI/CD 环境中，通常会设置此变量
		// 或者在本地运行测试时设置 TMDB_API_KEY=YOUR_API_KEY
		os.Stderr.WriteString("警告: TMDB_API_KEY 环境变量未设置。跳过集成测试。\n")
		// 将 globalAPIParams 设置为 nil，以便测试函数可以通过检查它来跳过
		globalAPIParams = nil
		// 运行所有测试，但由于 globalAPIParams 为 nil，所有依赖它的测试都会被跳过
		os.Exit(m.Run())
	}

	// 初始化全局的 TVService 实例
	globalTVService = service.NewTVService()
	// 初始化 url.Values
	globalAPIParams = url.Values{}
	// 添加 API 密钥
	globalAPIParams.Add("api_key", apiKey)
	// 元数据使用简体中文
	globalAPIParams.Add("language", "zh-CN")

	// 运行所有测试
	code := m.Run()
	// 退出
	os.Exit(code)
}

func TestTVService_GetTVDetail_Integration(t *testing.T) {
	// 如果 globalAPIParams 为 nil，说明 TMDB_API_KEY 未设置，跳过此测试
	if globalAPIParams == nil {
		t.Skip("TMDB_API_KEY 未设置，跳过集成测试")
	}

	tests := []struct {
		testName         string
		tvID             int
		minSeasons       int
		minSeasonsInfo   int
		minEpisodes      int
		seasonAssertions map[int]struct {
			minEpisodeCount int
			airDate         string
		}
	}{
		{
			testName:       "《葬送的芙莉莲》媒体详情",
			tvID:           209867,
			minSeasons:     2,
			minSeasonsInfo: 3,
			minEpisodes:    38,
			seasonAssertions: map[int]struct {
				minEpisodeCount int
				airDate         string
			}{
				0: {minEpisodeCount: 22, airDate: "2023-10-11"},
				1: {minEpisodeCount: 28, airDate: "2023-09-29"},
				2: {minEpisodeCount: 10, airDate: "2026-01-16"},
			},
		},
		{
			testName:       "《物语系列》媒体详情",
			tvID:           46195,
			minSeasons:     15,
			minSeasonsInfo: 16,
			minEpisodes:    72,
			seasonAssertions: map[int]struct {
				minEpisodeCount int
				airDate         string
			}{
				1:  {minEpisodeCount: 15, airDate: "2009-07-03"}, // 化物语 (12+3)
				2:  {minEpisodeCount: 11, airDate: "2012-01-08"}, // 伪物语 (原始)
				3:  {minEpisodeCount: 4, airDate: "2012-12-31"},  // 猫物语(黑) (0+4)
				13: {minEpisodeCount: 3, airDate: "2016-01-08"},  // 伤物语 (0+3) movie

			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()
			assert := assert.New(t)

			tvShow, err := globalTVService.GetTVDetail(context.Background(), tt.tvID, globalAPIParams)
			assert.Nil(err)
			assert.NotNil(tvShow)
			assert.True(tvShow.NumberOfSeasons >= tt.minSeasons)
			assert.True(len(tvShow.Seasons) >= tt.minSeasonsInfo)
			assert.True(tvShow.NumberOfEpisodes >= tt.minEpisodes)

			for _, season := range tvShow.Seasons {
				if assertion, ok := tt.seasonAssertions[season.SeasonNumber]; ok {
					assert.True(*season.EpisodeCount >= assertion.minEpisodeCount)
					assert.Equal(*season.AirDate, assertion.airDate)
					t.Logf("Season %d: Actual AirDate=%s, Expected AirDate=%s", season.SeasonNumber, *season.AirDate, assertion.airDate)
				}
			}
			t.Logf("GetTVDetail(%d): Name=%s, NumberOfSeasons=%d", tt.tvID, *tvShow.Name, tvShow.NumberOfSeasons)
		})
	}
}

func TestTVService_GetSeasonDetail_Integration(t *testing.T) {
	// 如果 globalAPIParams 为 nil，说明 TMDB_API_KEY 未设置，跳过此测试
	if globalAPIParams == nil {
		t.Skip("TMDB_API_KEY 未设置，跳过集成测试")
	}

	tests := []struct {
		testName     string
		tvID         int
		seasonNumber int
		minEpisodes  int
	}{
		{
			testName:     "《葬送的芙莉莲》 S02 季度转换",
			tvID:         209867,
			seasonNumber: 2,
			minEpisodes:  10,
		},
		{
			testName:     "《物语系列》 S13 季度转换",
			tvID:         46195,
			seasonNumber: 13,
			minEpisodes:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()
			assert := assert.New(t)

			season, err := globalTVService.GetSeasonDetail(context.Background(), tt.tvID, tt.seasonNumber, globalAPIParams)
			assert.Nil(err)
			assert.NotNil(season)
			assert.Equal(len(season.Episodes), tt.minEpisodes)
		})
	}
}

func TestTVService_GetEpisodeDetail_Integration(t *testing.T) {
	// 如果 globalAPIParams 为 nil，说明 TMDB_API_KEY 未设置，跳过此测试
	if globalAPIParams == nil {
		t.Skip("TMDB_API_KEY 未设置，跳过集成测试")
	}

	tests := []struct {
		testName      string
		tvID          int
		seasonNumber  int
		episodeNumber int
	}{
		{
			testName:      "《葬送的芙莉莲》 S02E01 集号转换",
			tvID:          209867,
			seasonNumber:  2,
			episodeNumber: 1,
		},
		{
			testName:      "《葬送的芙莉莲》 S01E028 集号转换",
			tvID:          209867,
			seasonNumber:  1,
			episodeNumber: 28,
		},
		{
			testName:      "《物语系列》 S01E15 集号转换",
			tvID:          46195,
			seasonNumber:  1,
			episodeNumber: 15,
		},
		{
			testName:      "《物语系列》 S13E01 集号转换",
			tvID:          46195,
			seasonNumber:  13,
			episodeNumber: 1,
		},
		{
			testName:      "《物语系列》 S02E01 集号转换",
			tvID:          46195,
			seasonNumber:  2,
			episodeNumber: 1,
		},
		{
			testName:      "《物语系列》 S11E01 集号转换",
			tvID:          46195,
			seasonNumber:  11,
			episodeNumber: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()
			assert := assert.New(t)

			episode, err := globalTVService.GetEpisodeDetail(context.Background(), tt.tvID, tt.seasonNumber, tt.episodeNumber, globalAPIParams)
			assert.Nil(err)
			assert.NotNil(episode)
			assert.Equal(tt.seasonNumber, episode.SeasonNumber)
			assert.Equal(tt.episodeNumber, episode.EpisodeNumber)
			t.Logf("GetEpisodeDetail(%d, %d, %d): Name=%s, SeasonNumber=%d, EpisodeNumber=%d", tt.tvID, tt.seasonNumber, tt.episodeNumber, *episode.Name, episode.SeasonNumber, episode.EpisodeNumber)
		})
	}
}
