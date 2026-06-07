package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/patrickmn/go-cache"

	"curetmdbanime/internal/config"
	"curetmdbanime/internal/logger"
	"curetmdbanime/internal/net"
)

// CureTMDb 缓存键和过期时间
const (
	cureTMDbCacheKey = "curetmdb_data"
	cureTMDbCacheTTL = 2 * time.Hour
)

// 结构体
type CureTMDb struct {
	sourceURL     string
	localFilePath string
	apiClient     *net.APIClient
	cache         *cache.Cache
}

// 创建一个新的 CureTMDb 实例
func NewCureTMDb() *CureTMDb {
	source := config.AppSettings.CureSource
	// 使用配置的数据目录和文件名
	localPath := filepath.Join(config.AppSettings.DataDir, "curetmdb.json")
	var sourceURL string
	// 检查配置的源 URL
	if len(source) > 0 && (source[0:4] == "http" || source[0:5] == "https") {
		sourceURL = source
	}

	c := &CureTMDb{
		sourceURL:     sourceURL,
		localFilePath: localPath,
		apiClient:     net.NewAPIClient(net.ClientOptions{UseProxy: true}),
		cache:         cache.New(cureTMDbCacheTTL, 10*time.Minute),
	}

	// 如果配置了本地文件路径，则启动后台任务定期从 URL 下载数据并更新本地文件
	if c.sourceURL != "" && c.localFilePath != "" {
		go c.startPeriodicUpdate()
	}

	return c
}

// 根据给定的 TMDB ID 获取季度信息
func (c *CureTMDb) GetSeasonInfo(ctx context.Context, tmdbID int) (map[string]any, error) {
	// 优先从缓存加载 CureTMDb 数据
	data, err := c.loadData(ctx)
	if err != nil {
		return nil, fmt.Errorf("GetSeasonInfo: 加载 CureTMDb 数据失败: %w", err)
	}
	// 尝试将 TMDB ID 对应的键值转换为 map[string]any 类型
	if tmdbIDStr, ok := data[fmt.Sprintf("%d", tmdbID)].(map[string]any); ok {
		return tmdbIDStr, nil
	}
	// 如果未找到对应的季度信息，返回空 map
	return map[string]any{}, nil
}

// 加载 CureTMDb 数据，优先从本地文件，然后从缓存
// 如果都没有，则从 URL 下载并保存到本地
func (c *CureTMDb) loadData(ctx context.Context) (map[string]any, error) {
	// 尝试从缓存中获取 CureTMDb 数据
	if cachedData, found := c.cache.Get(cureTMDbCacheKey); found {
		if data, ok := cachedData.(map[string]any); ok {
			logger.Debug("CureTMDb 数据从缓存加载成功")
			return data, nil
		}
	}

	// 尝试从本地文件加载数据
	if c.localFilePath != "" {
		logger.Info("正在从本地文件加载 CureTMDb 数据: %s", c.localFilePath)
		fileData, err := os.ReadFile(c.localFilePath)
		if err == nil {
			var data map[string]any
			if err := json.Unmarshal(fileData, &data); err != nil {
				logger.Error("本地文件 %s JSON 解码失败: %v", c.localFilePath, err)
				// 继续尝试从 URL 获取
			} else {
				// 将本地文件数据存入缓存
				c.cache.Set(cureTMDbCacheKey, data, cache.DefaultExpiration)
				logger.Info("CureTMDb 数据从本地文件加载并存入缓存成功")
				return data, nil
			}
		} else {
			logger.Warn("从本地文件 %s 读取 CureTMDb 数据失败: %v", c.localFilePath, err)
			// 文件不存在或读取失败，继续尝试从 URL 获取
		}
	}

	// 如果源 URL 为空，表示未配置数据源且本地文件不可用，返回空 map
	if c.sourceURL == "" {
		logger.Warn("未配置 CureTMDb 数据源 URL 或本地文件路径，且缓存中无数据，返回空数据。")
		return map[string]any{}, nil
	}

	// 从指定 URL 加载 CureTMDb 数据
	logger.Info("正在从 CureTMDb 数据源 URL 加载数据: %s", c.sourceURL)

	// 使用通用的 APIClient 发送 GET 请求
	respBody, err := c.apiClient.DoRequest(ctx, http.MethodGet, c.sourceURL, "", nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("加载 CureTMDb 数据失败: %w", err)
	}

	var data map[string]any
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, fmt.Errorf("CureTMDb 响应体 JSON 解码失败: %w", err)
	}

	// 将获取到的数据存入缓存
	c.cache.Set(cureTMDbCacheKey, data, cache.DefaultExpiration)
	logger.Info("CureTMDb 数据从 URL 加载成功并存入缓存")

	// 如果 localFilePath 已配置，则将数据保存到本地文件
	if c.localFilePath != "" {
		err = os.MkdirAll(filepath.Dir(c.localFilePath), 0755)
		if err != nil {
			logger.Error("创建 CureTMDb 本地数据目录失败: %v", err)
			// 不返回错误，继续使用已下载的数据
		} else {
			err = os.WriteFile(c.localFilePath, respBody, 0644)
			if err != nil {
				logger.Error("保存 CureTMDb 数据到本地文件 %s 失败: %v", c.localFilePath, err)
				// 不返回错误，继续使用已下载的数据
			} else {
				logger.Info("CureTMDb 数据已成功下载并保存到本地文件: %s", c.localFilePath)
			}
		}
	}

	return data, nil
}

// 启动一个 goroutine，定期从 sourceURL 下载数据并更新本地文件
func (c *CureTMDb) startPeriodicUpdate() {
	// 如果本地文件不存在，立即执行一次更新
	if _, err := os.Stat(c.localFilePath); os.IsNotExist(err) {
		logger.Info("本地文件 %s 不存在，立即执行一次更新。", c.localFilePath)
		c.updateLocalDataFromURL()
	} else if err != nil {
		logger.Error("检查本地文件 %s 状态失败: %v", c.localFilePath, err)
	} else {
		logger.Info("本地文件 %s 已存在，跳过立即更新。", c.localFilePath)
	}

	// 设置一个定时器，每隔 cureTMDbCacheTTL 执行一次更新
	ticker := time.NewTicker(cureTMDbCacheTTL)
	defer ticker.Stop()

	for range ticker.C {
		c.updateLocalDataFromURL()
	}
}

// 从 sourceURL 下载数据并保存到本地文件
func (c *CureTMDb) updateLocalDataFromURL() {
	if c.sourceURL == "" || c.localFilePath == "" {
		logger.Warn("未配置 CureTMDb 数据源 URL 或本地文件路径，跳过定期更新。")
		return
	}

	logger.Info("正在从 CureTMDb 数据源 URL 下载最新数据: %s", c.sourceURL)
	respBody, err := c.apiClient.DoRequest(context.Background(), http.MethodGet, c.sourceURL, "", nil, nil, nil)
	if err != nil {
		logger.Error("从 CureTMDb 数据源下载数据失败: %v", err)
		return
	}

	// 确保本地文件所在的目录存在
	err = os.MkdirAll(filepath.Dir(c.localFilePath), 0755)
	if err != nil {
		logger.Error("创建 CureTMDb 本地数据目录失败: %v", err)
		return
	}

	err = os.WriteFile(c.localFilePath, respBody, 0644)
	if err != nil {
		logger.Error("保存 CureTMDb 数据到本地文件 %s 失败: %v", c.localFilePath, err)
		return
	}
	logger.Info("CureTMDb 数据已成功下载并保存到本地文件: %s", c.localFilePath)
}
