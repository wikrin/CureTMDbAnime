package config

import (
	"fmt"
	"os"
	"strconv"
)

// 环境变量键
const (
	EnvKeyHost            = "HOST"
	EnvKeyPort            = "PORT"
	EnvKeyDebug           = "DEBUG"
	EnvKeyTmdbUpstreamURL = "TMDB_UPSTREAM_URL"
	EnvKeyCureSource      = "CURE_SOURCE"
	EnvKeyProxy           = "PROXY"
	EnvKeyUserAgent       = "USER_AGENT"
	EnvKeyDataDir         = "DATA_DIR"
)

// 默认值
const (
	DefaultHost            = "0.0.0.0"
	DefaultPort            = 8632
	DefaultDebug           = false
	DefaultTmdbUpstreamURL = "https://api.themoviedb.org"
	DefaultCureSource      = "https://raw.githubusercontent.com/wikrin/CureTMDb/main/tv.json"
	DefaultProxy           = ""
	DefaultDataDir         = "/opt/data"
)

// Settings 存储应用配置
type Settings struct {
	HOST              string
	PORT              int
	DEBUG             bool
	TMDB_UPSTREAM_URL string
	CURE_SOURCE       string
	PROXY             string
	DATA_DIR          string
}

// AppSettings 是全局配置实例
var AppSettings Settings

// LoadConfig 从环境变量加载配置
func LoadConfig() error {

	AppSettings = Settings{
		HOST:              getEnv(EnvKeyHost, DefaultHost),
		PORT:              getEnvAsInt(EnvKeyPort, DefaultPort),
		DEBUG:             getEnvAsBool(EnvKeyDebug, DefaultDebug),
		TMDB_UPSTREAM_URL: getEnv(EnvKeyTmdbUpstreamURL, DefaultTmdbUpstreamURL),
		CURE_SOURCE:       getEnv(EnvKeyCureSource, DefaultCureSource),
		PROXY:             getEnv(EnvKeyProxy, DefaultProxy),
		DATA_DIR:          getEnv(EnvKeyDataDir, DefaultDataDir),
	}
	return nil
}

// getEnv 获取环境变量字符串值，若无则返回默认值
func getEnv(key string, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// getEnvAsInt 获取环境变量整数值，若无则返回默认值
func getEnvAsInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		} else {
			fmt.Printf("警告: 环境变量 %s 的值 '%s' 无法转换为整数，使用默认值 %d: %v\n", key, value, defaultValue, err)
		}
	}
	return defaultValue
}

// getEnvAsBool 获取环境变量布尔值，若无则返回默认值
func getEnvAsBool(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		} else {
			fmt.Printf("警告: 环境变量 %s 的值 '%s' 无法转换为布尔值，使用默认值 %t: %v\n", key, value, defaultValue, err)
		}
	}
	return defaultValue
}
