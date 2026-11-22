package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// 应用配置。新增普通配置只需在这里声明类型、参数名、默认值和说明。
type Settings struct {
	Host              string   `option:"host" default:"0.0.0.0" usage:"服务监听地址"`
	Port              int      `option:"port" default:"8632" usage:"服务监听端口"`
	Debug             bool     `option:"debug" default:"false" usage:"启用调试日志"`
	TmdbAPIURL        string   `option:"tmdb-api-url" default:"https://api.themoviedb.org" usage:"TMDB API 上游地址"`
	CureSource        string   `option:"cure-source" default:"https://raw.githubusercontent.com/wikrin/CureTMDb/main/tv.json" usage:"修正规则的源数据 URL (JSON)"`
	Proxy             string   `option:"proxy" default:"" usage:"可选：请求上游时使用的 HTTP/HTTPS 代理" sensitive:"true"`
	DataDir           string   `option:"data-dir" default:"/opt/data" usage:"数据存储目录"`
	BangumiAPIURL     string   `option:"bangumi-api-url" default:"https://api.bgm.tv/" usage:"Bangumi API 上游地址"`
	BangumiUseProxy   bool     `option:"bangumi-use-proxy" default:"false" usage:"Bangumi API 请求是否使用 PROXY"`
	ReferencePriority []string `option:"reference-priority" default:"tvdb,bangumi" usage:"分季参照源优先级，逗号分隔"`
	TVDBAPIKey        string   `option:"tvdb-api-key" default:"d3158670-26f0-48bb-9267-2f63781dca6b" usage:"TVDB API 密钥，可覆盖内置密钥" sensitive:"true"`
	TVDBAPIURL        string   `option:"tvdb-api-url" default:"https://api4.thetvdb.com/v4" usage:"TVDB API 上游地址"`
}

var DefaultSettings = newDefaultSettings()

// 分别构造默认值，避免列表字段与 DefaultSettings 共享底层数组。
var AppSettings = newDefaultSettings()

func newDefaultSettings() Settings {
	var settings Settings
	value := reflect.ValueOf(&settings).Elem()
	for _, option := range settingsOptions {
		decoded, err := decodeOptionValue(option.typ, option.defaultValue)
		if err != nil {
			panic(err) // 配置定义已在初始化时检查。
		}
		value.Field(option.index).Set(reflect.ValueOf(decoded))
	}
	return settings
}

// 接收 CLI 层已解析的配置参数，优先级: Default < ENV < CLI。
// 空环境变量保持 Viper 的原有行为：视为未设置；CLI 可显式传入空值。
func LoadConfig(flagSet *pflag.FlagSet) error {
	cfg := viper.New()
	defaults := reflect.ValueOf(DefaultSettings)
	for _, option := range settingsOptions {
		cfg.SetDefault(option.name, defaults.Field(option.index).Interface())
		if err := cfg.BindEnv(option.name, option.env); err != nil {
			return fmt.Errorf("绑定环境变量 %s 失败: %w", option.env, err)
		}
	}
	if flagSet != nil {
		if err := cfg.BindPFlags(flagSet); err != nil {
			return fmt.Errorf("绑定命令行参数失败: %w", err)
		}
	}

	var settings Settings
	value := reflect.ValueOf(&settings).Elem()
	for _, option := range settingsOptions {
		decoded, err := decodeOptionValue(option.typ, cfg.Get(option.name))
		if err != nil {
			return fmt.Errorf("配置 --%s / %s 无效: %w", option.name, option.env, err)
		}
		value.Field(option.index).Set(reflect.ValueOf(decoded))
	}
	if err := settings.NormalizeAndValidate(); err != nil {
		return err
	}
	// 所有转换和校验都成功后才更新全局配置。
	AppSettings = settings
	return nil
}

func NewFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("option", pflag.ContinueOnError)
	defaults := reflect.ValueOf(DefaultSettings)
	for _, option := range settingsOptions {
		option.registerFlag(flags, defaults.Field(option.index).Interface())
	}
	return flags
}

// NormalizeAndValidate 集中处理业务规则，不参与通用的配置来源和类型转换。
func (s *Settings) NormalizeAndValidate() error {
	references, err := ParseReferencePriority(strings.Join(s.ReferencePriority, ","))
	if err != nil {
		return fmt.Errorf("配置 --reference-priority / REFERENCE_PRIORITY 无效: %w", err)
	}
	s.ReferencePriority = references
	return nil
}

// ParseReferencePriority parses the configurable references after CureTMDb.
func ParseReferencePriority(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = strings.Join(DefaultSettings.ReferencePriority, ",")
	}

	allowed := map[string]struct{}{"bangumi": {}, "tvdb": {}}
	seen := make(map[string]struct{})
	references := make([]string, 0, 2)
	for item := range strings.SplitSeq(raw, ",") {
		reference := strings.ToLower(strings.TrimSpace(item))
		if reference == "" {
			return nil, fmt.Errorf("分季依据优先级包含空项")
		}
		if reference == "curetmdb" {
			return nil, fmt.Errorf("curetmdb 的优先级固定最高，不允许配置")
		}
		if _, ok := allowed[reference]; !ok {
			return nil, fmt.Errorf("不支持的分季依据: %s，可选值为 bangumi,tvdb", reference)
		}
		if _, ok := seen[reference]; ok {
			return nil, fmt.Errorf("分季依据重复配置: %s", reference)
		}
		seen[reference] = struct{}{}
		references = append(references, reference)
	}
	return references, nil
}
