package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"

	"curetmdbanime/internal/collection"
	"curetmdbanime/internal/config"
	"curetmdbanime/internal/logger"
	"curetmdbanime/internal/model"
	"curetmdbanime/internal/net"
)

const (
	TVDBCacheTTL          = 12 * time.Hour
	TVDBTokenRefreshAhead = 24 * time.Hour
	TVDBTokenTTL          = 30 * 24 * time.Hour
)

// TVDBClient provides season splitting data from TheTVDB.
type TVDBClient struct {
	apiClient *net.APIClient
	baseURL   string
	apiKey    string
	pin       string
	cache     *cache.Cache

	tokenMu  sync.Mutex
	token    string
	tokenExp time.Time
}

func NewTVDBClient() *TVDBClient {
	baseURL := strings.TrimRight(strings.TrimSpace(config.AppSettings.TVDBAPIURL), "/")
	if baseURL == "" {
		baseURL = config.DefaultTVDBAPIURL
	}

	return &TVDBClient{
		apiClient: net.NewAPIClient(net.ClientOptions{UseProxy: true}),
		baseURL:   baseURL,
		apiKey:    strings.TrimSpace(config.AppSettings.TVDBAPIKey),
		pin:       strings.TrimSpace(config.AppSettings.TVDBPIN),
		cache:     cache.New(TVDBCacheTTL, 10*time.Minute),
	}
}

func (t *TVDBClient) GetSeriesEpisodes(ctx context.Context, tvdbID int) (*model.SeriesEntry, error) {
	if t.apiKey == "" {
		return nil, nil
	}

	cacheKey := fmt.Sprintf("tvdb_series_%d", tvdbID)
	if cached, found := t.cache.Get(cacheKey); found {
		if entry, ok := cached.(*model.SeriesEntry); ok {
			return entry, nil
		}
	}

	entry, err := t.fetchSeriesEntry(ctx, tvdbID)
	if err != nil {
		return nil, err
	}
	if entry != nil {
		t.cache.Set(cacheKey, entry, cache.DefaultExpiration)
	}
	return entry, nil
}

func (t *TVDBClient) fetchSeriesEntry(ctx context.Context, seriesID int) (*model.SeriesEntry, error) {
	response := make(map[string]any)
	params := url.Values{}
	params.Set("page", "0")

	if err := t.doJSON(ctx, http.MethodGet, fmt.Sprintf("/series/%d/episodes/official/eng", seriesID), nil, params, &response); err != nil {
		return nil, fmt.Errorf("TVDB 系列详情查询失败: %w", err)
	}

	data, ok := response["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("TVDB 响应缺少有效的 data 字段")
	}
	episodes, ok := data["episodes"].([]any)
	if !ok || len(episodes) == 0 {
		return nil, nil
	}

	seasons := make(map[int]*model.SeasonEntry)
	for _, rawEpisode := range episodes {
		episode, ok := rawEpisode.(map[string]any)
		if !ok {
			continue
		}
		seasonNumber := collection.GetInt(episode, "seasonNumber")
		episodeNumber := collection.GetInt(episode, "number")
		if seasonNumber <= 0 || episodeNumber <= 0 {
			continue
		}
		season, ok := seasons[seasonNumber]
		if !ok {
			season = &model.SeasonEntry{
				SeasonNumber: seasonNumber,
				EpisodeCount: 0,
			}
			seasons[seasonNumber] = season
		}
		season.EpisodeCount += 1
	}

	if len(seasons) == 0 {
		return nil, nil
	}
	result := make([]*model.SeasonEntry, 0, len(seasons))
	for _, season := range seasons {
		result = append(result, season)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].SeasonNumber < result[j].SeasonNumber
	})
	return &model.SeriesEntry{Seasons: result}, nil
}

func (t *TVDBClient) doJSON(ctx context.Context, method, endpoint string, body any, params url.Values, result any) error {
	token, err := t.getToken(ctx)
	if err != nil {
		return err
	}
	headers := map[string]string{"Authorization": "Bearer " + token}
	bodyBytes, err := t.apiClient.DoRequest(ctx, method, t.baseURL, endpoint, body, params, headers)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(bodyBytes, result); err != nil {
		return fmt.Errorf("TVDB 响应解码失败: %w", err)
	}
	return nil
}

func (t *TVDBClient) getToken(ctx context.Context) (string, error) {
	t.tokenMu.Lock()
	defer t.tokenMu.Unlock()
	if t.token != "" && time.Now().Add(TVDBTokenRefreshAhead).Before(t.tokenExp) {
		return t.token, nil
	}

	body := map[string]string{"apikey": t.apiKey}
	if t.pin != "" {
		body["pin"] = t.pin
	}
	responseBody, err := t.apiClient.DoRequest(ctx, http.MethodPost, t.baseURL, "/login", body, nil, nil)
	if err != nil {
		return "", fmt.Errorf("TVDB 登录失败: %w", err)
	}
	response := make(map[string]any)
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("TVDB 登录响应解码失败: %w", err)
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("TVDB 登录响应缺少有效的 data 字段")
	}
	token, ok := data["token"].(string)
	if !ok || token == "" {
		return "", fmt.Errorf("TVDB 登录响应缺少 token")
	}
	t.token = token
	t.tokenExp = time.Now().Add(TVDBTokenTTL)
	logger.Debug("TVDB token 获取成功")
	return t.token, nil
}
