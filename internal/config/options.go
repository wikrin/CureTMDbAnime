package config

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

// optionDefinition 是从 Settings 生成的元信息，不再手动维护配置注册表。
type optionDefinition struct {
	index        int
	name         string
	env          string
	usage        string
	typ          reflect.Type
	defaultValue any
	sensitive    bool
}

var settingsOptions = mustDescribeOptions(reflect.TypeFor[Settings]())

func mustDescribeOptions(typ reflect.Type) []optionDefinition {
	options, err := describeOptions(typ)
	if err != nil {
		panic(err) // 配置定义错误属于开发错误，启动时立即发现。
	}
	return options
}

func describeOptions(typ reflect.Type) ([]optionDefinition, error) {
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("配置类型必须是结构体: %s", typ)
	}
	options := make([]optionDefinition, 0, typ.NumField())
	names, envs := make(map[string]bool), make(map[string]bool)
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		name := field.Tag.Get("option")
		if name == "-" {
			continue
		}
		if name == "" || name != strings.ToLower(name) || !field.IsExported() {
			return nil, fmt.Errorf("配置字段 %s 必须导出并声明小写 option 标签（或 option:\"-\" 跳过）", field.Name)
		}
		env := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
		if names[name] || envs[env] {
			return nil, fmt.Errorf("配置字段 %s 的参数名或环境变量名重复: %s / %s", field.Name, name, env)
		}
		names[name], envs[env] = true, true
		usage := field.Tag.Get("usage")
		if strings.TrimSpace(usage) == "" {
			return nil, fmt.Errorf("配置字段 %s 缺少 usage 标签", field.Name)
		}
		rawDefault, ok := field.Tag.Lookup("default")
		if !ok {
			return nil, fmt.Errorf("配置字段 %s 缺少 default 标签，空默认值请使用 default:\"\"", field.Name)
		}
		defaultValue, err := decodeOptionValue(field.Type, rawDefault)
		if err != nil {
			return nil, fmt.Errorf("配置字段 %s 的默认值无效: %w", field.Name, err)
		}
		sensitive := false
		if raw, ok := field.Tag.Lookup("sensitive"); ok {
			sensitive, err = strconv.ParseBool(raw)
			if err != nil {
				return nil, fmt.Errorf("配置字段 %s 的 sensitive 标签必须是布尔值", field.Name)
			}
		}
		options = append(options, optionDefinition{
			index: index, name: name, env: env, usage: usage,
			typ: field.Type, defaultValue: defaultValue, sensitive: sensitive,
		})
	}
	return options, nil
}

// decodeOptionValue 严格转换原始 ENV/CLI 值和已转换的默认值。
// 错误不回显输入，避免未来敏感配置因转换失败泄露。
func decodeOptionValue(typ reflect.Type, raw any) (any, error) {
	switch typ {
	case reflect.TypeFor[string]():
		if value, ok := raw.(string); ok {
			return value, nil
		}
		return nil, fmt.Errorf("需要字符串")
	case reflect.TypeFor[int]():
		if value, ok := raw.(int); ok {
			return value, nil
		}
		if value, ok := raw.(string); ok {
			if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				return parsed, nil
			}
		}
		return nil, fmt.Errorf("需要有效整数")
	case reflect.TypeFor[bool]():
		if value, ok := raw.(bool); ok {
			return value, nil
		}
		if value, ok := raw.(string); ok {
			if parsed, err := strconv.ParseBool(strings.TrimSpace(value)); err == nil {
				return parsed, nil
			}
		}
		return nil, fmt.Errorf("需要布尔值（true/false）")
	case reflect.TypeFor[[]string]():
		if value, ok := raw.([]string); ok {
			return slices.Clone(value), nil
		}
		if value, ok := raw.(string); ok {
			if strings.TrimSpace(value) == "" {
				return []string(nil), nil
			}
			items := strings.Split(value, ",")
			for index := range items {
				items[index] = strings.TrimSpace(items[index])
			}
			return items, nil
		}
		return nil, fmt.Errorf("需要逗号分隔的字符串列表")
	default:
		return nil, fmt.Errorf("不支持的配置字段类型: %s", typ)
	}
}

func (option optionDefinition) registerFlag(flags *pflag.FlagSet, value any) {
	switch value := value.(type) {
	case string:
		flags.String(option.name, value, option.usage)
	case int:
		flags.Int(option.name, value, option.usage)
	case bool:
		flags.Bool(option.name, value, option.usage)
	case []string:
		// 保持现有逗号分隔语法，不引入 pflag StringSlice 的 CSV/重复参数语义。
		flags.String(option.name, strings.Join(value, ","), option.usage)
	default:
		panic(fmt.Sprintf("不支持的配置字段类型: %T", value))
	}
	if option.sensitive && optionValueString(value) != "" {
		// DefValue 仅用于帮助文本，不改变实际参数值及 Viper 的默认值。
		flags.Lookup(option.name).DefValue = "已隐藏"
	}
}

func optionValueString(value any) string {
	if items, ok := value.([]string); ok {
		return strings.Join(items, ",")
	}
	return fmt.Sprint(value)
}
