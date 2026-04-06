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
	cfg.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

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

	bindings := []struct {
		key    string
		envKey string
	}{
		{key: configKeyHost, envKey: EnvKeyHost},
		{key: configKeyPort, envKey: EnvKeyPort},
		{key: configKeyDebug, envKey: EnvKeyDebug},
		{key: configKeyTmdbAPIURL, envKey: EnvKeyTmdbAPIURL},
		{key: configKeyCureSource, envKey: EnvKeyCureSource},
		{key: configKeyProxy, envKey: EnvKeyProxy},
		{key: configKeyDataDir, envKey: EnvKeyDataDir},
	}

	for _, binding := range bindings {
		if err := bindEnv(cfg, binding.key, binding.envKey); err != nil {
			return err
		}
	}
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

func bindEnv(cfg *viper.Viper, key, envKey string) error {
	if err := cfg.BindEnv(key, envKey); err != nil {
		return fmt.Errorf("绑定环境变量失败: key=%s env=%s err=%w", key, envKey, err)
	}

	return nil
}
