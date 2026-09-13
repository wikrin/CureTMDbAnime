package config

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// 环境变量键
const (
	EnvKeyHost                   = "HOST"
	EnvKeyPort                   = "PORT"
	EnvKeyDebug                  = "DEBUG"
	EnvKeyTmdbAPIURL             = "TMDB_API_URL"
	EnvKeyCureSource             = "CURE_SOURCE"
	EnvKeyProxy                  = "PROXY"
	EnvKeyDataDir                = "DATA_DIR"
	EnvKeyBangumiAPIURL          = "BANGUMI_API_URL"
	EnvKeyBangumiUseProxy        = "BANGUMI_USE_PROXY"
	EnvKeySeasonProviderPriority = "SEASON_PROVIDER_PRIORITY"
	EnvKeyTVDBAPIKey             = "TVDB_API_KEY"
	EnvKeyTVDBPIN                = "TVDB_PIN"
	EnvKeyTVDBAPIURL             = "TVDB_API_URL"
)

// 默认值
const (
	DefaultHost                   = "0.0.0.0"
	DefaultPort                   = 8632
	DefaultDebug                  = false
	DefaultTmdbAPIURL             = "https://api.themoviedb.org"
	DefaultCureSource             = "https://raw.githubusercontent.com/wikrin/CureTMDb/main/tv.json"
	DefaultProxy                  = ""
	DefaultDataDir                = "/opt/data"
	DefaultBangumiAPIURL          = "https://api.bgm.tv/"
	DefaultBangumiUseProxy        = false
	DefaultSeasonProviderPriority = "tvdb,bangumi"
	DefaultTVDBAPIURL             = "https://api4.thetvdb.com/v4"
)

const (
	configKeyHost                   = "host"
	configKeyPort                   = "port"
	configKeyDebug                  = "debug"
	configKeyTmdbAPIURL             = "tmdb-api-url"
	configKeyCureSource             = "cure-source"
	configKeyProxy                  = "proxy"
	configKeyDataDir                = "data-dir"
	configKeyBangumiAPIURL          = "bangumi-api-url"
	configKeyBangumiUseProxy        = "bangumi-use-proxy"
	configKeySeasonProviderPriority = "season-provider-priority"
	configKeyTVDBAPIKey             = "tvdb-api-key"
	configKeyTVDBPIN                = "tvdb-pin"
	configKeyTVDBAPIURL             = "tvdb-api-url"
)

// 应用配置
type Settings struct {
	Host                   string
	Port                   int
	Debug                  bool
	TmdbAPIURL             string
	CureSource             string
	Proxy                  string
	DataDir                string
	BangumiAPIURL          string
	BangumiUseProxy        bool
	SeasonProviderPriority []string
	TVDBAPIKey             string
	TVDBPIN                string
	TVDBAPIURL             string
}

// 全局配置实例
var AppSettings = Settings{
	Host:                   DefaultHost,
	Port:                   DefaultPort,
	Debug:                  DefaultDebug,
	TmdbAPIURL:             DefaultTmdbAPIURL,
	CureSource:             DefaultCureSource,
	Proxy:                  DefaultProxy,
	DataDir:                DefaultDataDir,
	BangumiAPIURL:          DefaultBangumiAPIURL,
	BangumiUseProxy:        DefaultBangumiUseProxy,
	SeasonProviderPriority: []string{"bangumi", "tvdb"},
	TVDBAPIURL:             DefaultTVDBAPIURL,
}

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
	cfg.SetDefault(configKeyBangumiAPIURL, DefaultBangumiAPIURL)
	cfg.SetDefault(configKeyBangumiUseProxy, DefaultBangumiUseProxy)
	cfg.SetDefault(configKeySeasonProviderPriority, DefaultSeasonProviderPriority)
	cfg.SetDefault(configKeyTVDBAPIKey, TVDBAPIKey)
	cfg.SetDefault(configKeyTVDBAPIURL, DefaultTVDBAPIURL)

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
		{key: configKeyBangumiAPIURL, envKey: EnvKeyBangumiAPIURL},
		{key: configKeyBangumiUseProxy, envKey: EnvKeyBangumiUseProxy},
		{key: configKeySeasonProviderPriority, envKey: EnvKeySeasonProviderPriority},
		{key: configKeyTVDBAPIKey, envKey: EnvKeyTVDBAPIKey},
		{key: configKeyTVDBPIN, envKey: EnvKeyTVDBPIN},
		{key: configKeyTVDBAPIURL, envKey: EnvKeyTVDBAPIURL},
	}

	for _, binding := range bindings {
		if err := bindEnv(cfg, binding.key, binding.envKey); err != nil {
			return err
		}
	}
	cfg.AutomaticEnv()

	seasonProviderPriority, err := ParseSeasonProviderPriority(cfg.GetString(configKeySeasonProviderPriority))
	if err != nil {
		return err
	}

	AppSettings = Settings{
		Host:                   cfg.GetString(configKeyHost),
		Port:                   cfg.GetInt(configKeyPort),
		Debug:                  cfg.GetBool(configKeyDebug),
		TmdbAPIURL:             cfg.GetString(configKeyTmdbAPIURL),
		CureSource:             cfg.GetString(configKeyCureSource),
		Proxy:                  cfg.GetString(configKeyProxy),
		DataDir:                cfg.GetString(configKeyDataDir),
		BangumiAPIURL:          cfg.GetString(configKeyBangumiAPIURL),
		BangumiUseProxy:        cfg.GetBool(configKeyBangumiUseProxy),
		SeasonProviderPriority: seasonProviderPriority,
		TVDBAPIKey:             cfg.GetString(configKeyTVDBAPIKey),
		TVDBPIN:                cfg.GetString(configKeyTVDBPIN),
		TVDBAPIURL:             cfg.GetString(configKeyTVDBAPIURL),
	}

	return nil
}

// ParseSeasonProviderPriority parses the configurable providers after CureTMDb.
func ParseSeasonProviderPriority(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = DefaultSeasonProviderPriority
	}

	allowed := map[string]struct{}{"bangumi": {}, "tvdb": {}}
	seen := make(map[string]struct{})
	providers := make([]string, 0, 2)
	for item := range strings.SplitSeq(raw, ",") {
		provider := strings.ToLower(strings.TrimSpace(item))
		if provider == "" {
			return nil, fmt.Errorf("分季 provider 优先级包含空项")
		}
		if provider == "curetmdb" {
			return nil, fmt.Errorf("curetmdb 的优先级固定最高，不允许配置")
		}
		if _, ok := allowed[provider]; !ok {
			return nil, fmt.Errorf("不支持的分季 provider: %s，可选值为 bangumi,tvdb", provider)
		}
		if _, ok := seen[provider]; ok {
			return nil, fmt.Errorf("分季 provider 重复配置: %s", provider)
		}
		seen[provider] = struct{}{}
		providers = append(providers, provider)
	}
	return providers, nil
}

func bindEnv(cfg *viper.Viper, key, envKey string) error {
	if err := cfg.BindEnv(key, envKey); err != nil {
		return fmt.Errorf("绑定环境变量失败: key=%s env=%s err=%w", key, envKey, err)
	}

	return nil
}
