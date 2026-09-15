package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// 应用配置
type Settings struct {
	Host              string   `option:"host"`
	Port              int      `option:"port"`
	Debug             bool     `option:"debug"`
	TmdbAPIURL        string   `option:"tmdb-api-url"`
	CureSource        string   `option:"cure-source"`
	Proxy             string   `option:"proxy"`
	DataDir           string   `option:"data-dir"`
	BangumiAPIURL     string   `option:"bangumi-api-url"`
	BangumiUseProxy   bool     `option:"bangumi-use-proxy"`
	ReferencePriority []string `option:"reference-priority"`
	TVDBAPIKey        string   `option:"tvdb-api-key"`
	TVDBAPIURL        string   `option:"tvdb-api-url"`
}

var DefaultSettings = Settings{
	Host:              "0.0.0.0",
	Port:              8632,
	TmdbAPIURL:        "https://api.themoviedb.org",
	CureSource:        "https://raw.githubusercontent.com/wikrin/CureTMDb/main/tv.json",
	DataDir:           "/opt/data",
	BangumiAPIURL:     "https://api.bgm.tv/",
	ReferencePriority: []string{"tvdb", "bangumi"},
	TVDBAPIKey:        "d3158670-26f0-48bb-9267-2f63781dca6b",
	TVDBAPIURL:        "https://api4.thetvdb.com/v4",
}

var AppSettings = DefaultSettings

// 接收 CLI 层已解析的配置参数，优先级: Default < ENV < CLI
func LoadConfig(flagSet *pflag.FlagSet) error {
	cfg := viper.New()
	cfg.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	defaults := DefaultSettings
	forEachConfigField(reflect.TypeFor[Settings](), reflect.ValueOf(defaults), func(field reflect.StructField, name string, value reflect.Value) {
		if name == "reference-priority" {
			cfg.SetDefault(name, strings.Join(value.Interface().([]string), ","))
			return
		}
		cfg.SetDefault(name, value.Interface())
	})

	if flagSet != nil {
		if err := cfg.BindPFlags(flagSet); err != nil {
			return fmt.Errorf("绑定命令行参数失败: %w", err)
		}
	}

	cfg.BindEnv("reference-priority", "REFERENCE_PRIORITY")
	cfg.AutomaticEnv()
	forEachConfigField(reflect.TypeFor[Settings](), reflect.ValueOf(defaults), func(field reflect.StructField, name string, value reflect.Value) {
		cfg.Set(name, cfg.Get(name))
	})

	referencePriority, err := ParseReferencePriority(cfg.GetString("reference-priority"))
	if err != nil {
		return err
	}
	cfg.Set("reference-priority", referencePriority)

	settings := defaults
	forEachConfigField(reflect.TypeFor[Settings](), reflect.ValueOf(&settings).Elem(), func(field reflect.StructField, name string, value reflect.Value) {
		switch field.Type.Kind() {
		case reflect.String:
			value.SetString(cfg.GetString(name))
		case reflect.Int:
			value.SetInt(int64(cfg.GetInt(name)))
		case reflect.Bool:
			value.SetBool(cfg.GetBool(name))
		}
	})
	settings.ReferencePriority = referencePriority
	AppSettings = settings

	return nil
}

func NewFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("option", pflag.ContinueOnError)
	forEachConfigField(reflect.TypeFor[Settings](), reflect.Value{}, func(field reflect.StructField, name string, value reflect.Value) {
		switch field.Type.Kind() {
		case reflect.String:
			flags.String(name, "", "")
		case reflect.Int:
			flags.Int(name, 0, "")
		case reflect.Bool:
			flags.Bool(name, false, "")
		case reflect.Slice:
			flags.String(name, "", "")
		default:
			panic(fmt.Sprintf("不支持的配置字段类型: %s", field.Type))
		}
	})
	return flags
}

func forEachConfigField(typ reflect.Type, value reflect.Value, fn func(reflect.StructField, string, reflect.Value)) {
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		name := field.Tag.Get("option")
		if name == "" || name == "-" {
			continue
		}
		fieldValue := reflect.Value{}
		if value.IsValid() {
			fieldValue = value.Field(index)
		}
		fn(field, name, fieldValue)
	}
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
