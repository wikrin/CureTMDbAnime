package config

import (
	"fmt"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
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

	// 设置默认值
	viper.SetDefault(EnvKeyHost, DefaultHost)
	viper.SetDefault(EnvKeyPort, DefaultPort)
	viper.SetDefault(EnvKeyDebug, DefaultDebug)
	viper.SetDefault(EnvKeyTmdbUpstreamURL, DefaultTmdbUpstreamURL)
	viper.SetDefault(EnvKeyCureSource, DefaultCureSource)
	viper.SetDefault(EnvKeyProxy, DefaultProxy)
	viper.SetDefault(EnvKeyDataDir, DefaultDataDir)

	// 定义命令行参数并绑定到 viper
	pflag.String(EnvKeyHost, DefaultHost, "服务监听地址")
	pflag.Int(EnvKeyPort, DefaultPort, "服务监听端口")
	pflag.Bool(EnvKeyDebug, DefaultDebug, "启用调试模式")
	pflag.String(EnvKeyTmdbUpstreamURL, DefaultTmdbUpstreamURL, "TMDB 上游 API URL")
	pflag.String(EnvKeyCureSource, DefaultCureSource, "CureTMDb 数据源 URL")
	pflag.String(EnvKeyProxy, DefaultProxy, "HTTP/HTTPS 代理地址 (例如: http://127.0.0.1:7890)")
	pflag.String(EnvKeyDataDir, DefaultDataDir, "数据存储目录")
	pflag.Parse() // 解析命令行参数

	err := viper.BindPFlags(pflag.CommandLine)
	if err != nil {
		return fmt.Errorf("绑定命令行参数失败: %w", err)
	}

	// 配置 Viper 从环境变量读取
	viper.AutomaticEnv() // 自动将环境变量绑定到匹配的键

	// 将配置值填充到 AppSettings 结构体
	AppSettings = Settings{
		HOST:              viper.GetString(EnvKeyHost),
		PORT:              viper.GetInt(EnvKeyPort),
		DEBUG:             viper.GetBool(EnvKeyDebug),
		TMDB_UPSTREAM_URL: viper.GetString(EnvKeyTmdbUpstreamURL),
		CURE_SOURCE:       viper.GetString(EnvKeyCureSource),
		PROXY:             viper.GetString(EnvKeyProxy),
		DATA_DIR:          viper.GetString(EnvKeyDataDir),
	}

	return nil
}
