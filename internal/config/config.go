package config

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// 环境变量键
const (
	EnvKeyHost       = "HOST"
	EnvKeyPort       = "PORT"
	EnvKeyDebug      = "DEBUG"
	EnvKeyTmdbAPIURL = "TMDB_API_URL"
	EnvKeyCureSource = "CURE_SOURCE"
	EnvKeyProxy      = "PROXY"
	EnvKeyUserAgent  = "USER_AGENT"
	EnvKeyDataDir    = "DATA_DIR"
)

// 默认值
const (
	DefaultHost       = "0.0.0.0"
	DefaultPort       = 8632
	DefaultDebug      = false
	DefaultTmdbAPIURL = "https://api.themoviedb.org"
	DefaultCureSource = "https://raw.githubusercontent.com/wikrin/CureTMDb/main/tv.json"
	DefaultProxy      = ""
	DefaultDataDir    = "/opt/data"
)

const (
	configKeyHost       = "host"
	configKeyPort       = "port"
	configKeyDebug      = "debug"
	configKeyTmdbAPIURL = "tmdb-api-url"
	configKeyCureSource = "cure-source"
	configKeyProxy      = "proxy"
	configKeyDataDir    = "data-dir"
)

// 应用配置
type Settings struct {
	Host       string
	Port       int
	Debug      bool
	TmdbAPIURL string
	CureSource string
	Proxy      string
	DataDir    string
}

// 全局配置实例
var AppSettings Settings

// 接收 CLI 层已解析的配置参数，优先级: Default < ENV < CLI
func LoadConfig(flagSet *pflag.FlagSet) error {
	cfg := viper.New()

	cfg.SetDefault(configKeyHost, DefaultHost)
	cfg.SetDefault(configKeyPort, DefaultPort)
	cfg.SetDefault(configKeyDebug, DefaultDebug)
	cfg.SetDefault(configKeyTmdbAPIURL, DefaultTmdbAPIURL)
	cfg.SetDefault(configKeyCureSource, DefaultCureSource)
	cfg.SetDefault(configKeyProxy, DefaultProxy)
	cfg.SetDefault(configKeyDataDir, DefaultDataDir)

	if flagSet != nil {
		if err := cfg.BindPFlags(flagSet); err != nil {
			return fmt.Errorf("绑定命令行参数失败: %w", err)
		}
	}

	bindEnv(cfg, configKeyHost, EnvKeyHost)
	bindEnv(cfg, configKeyPort, EnvKeyPort)
	bindEnv(cfg, configKeyDebug, EnvKeyDebug)
	bindEnv(cfg, configKeyTmdbAPIURL, EnvKeyTmdbAPIURL)
	bindEnv(cfg, configKeyCureSource, EnvKeyCureSource)
	bindEnv(cfg, configKeyProxy, EnvKeyProxy)
	bindEnv(cfg, configKeyDataDir, EnvKeyDataDir)
	cfg.AutomaticEnv()

	AppSettings = Settings{
		Host:       cfg.GetString(configKeyHost),
		Port:       cfg.GetInt(configKeyPort),
		Debug:      cfg.GetBool(configKeyDebug),
		TmdbAPIURL: cfg.GetString(configKeyTmdbAPIURL),
		CureSource: cfg.GetString(configKeyCureSource),
		Proxy:      cfg.GetString(configKeyProxy),
		DataDir:    cfg.GetString(configKeyDataDir),
	}

	return nil
}

func bindEnv(cfg *viper.Viper, key, envKey string) {
	if err := cfg.BindEnv(key, envKey); err != nil {
		panic(fmt.Sprintf("绑定环境变量失败: key=%s env=%s err=%v", key, envKey, err))
	}

	cfg.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
}
