package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func isolateConfig(t *testing.T) {
	t.Helper()
	oldSettings, oldDefaults := AppSettings, DefaultSettings
	t.Cleanup(func() { AppSettings, DefaultSettings = oldSettings, oldDefaults })
	DefaultSettings, AppSettings = newDefaultSettings(), newDefaultSettings()
	for _, option := range settingsOptions {
		t.Setenv(option.env, "")
	}
}

func TestLoadDefaults(t *testing.T) {
	for _, mode := range []string{"nil flags", "unparsed flags", "parsed empty flags"} {
		t.Run(mode, func(t *testing.T) {
			isolateConfig(t)
			var flags *pflag.FlagSet
			if mode != "nil flags" {
				flags = NewFlagSet()
				if mode == "parsed empty flags" {
					if err := flags.Parse(nil); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := LoadConfig(flags); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(AppSettings, DefaultSettings) {
				t.Fatalf("got %#v, want defaults %#v", AppSettings, DefaultSettings)
			}
			if AppSettings.Host != "0.0.0.0" || AppSettings.Port != 8632 {
				t.Fatal("existing listening defaults changed")
			}
		})
	}
}

func sampleOptionValue(option optionDefinition, cli bool) (string, any) {
	switch option.typ {
	case reflect.TypeFor[string]():
		prefix := "env-"
		if cli {
			prefix = "cli-"
		}
		return prefix + option.name, prefix + option.name
	case reflect.TypeFor[int]():
		if cli {
			return "9002", 9002
		}
		return "9001", 9001
	case reflect.TypeFor[bool]():
		if cli {
			return "false", false
		}
		return "true", true
	case reflect.TypeFor[[]string]():
		if cli {
			return " bangumi, tvdb ", []string{"bangumi", "tvdb"}
		}
		return "tvdb", []string{"tvdb"}
	default:
		panic("add a test sample for the new supported type")
	}
}

// 遍历定义而非维护字段清单，新增普通配置会自动覆盖 ENV/CLI 的加载测试。
func TestLoadEveryEnvironmentOption(t *testing.T) {
	for _, option := range settingsOptions {
		for _, withFlags := range []bool{false, true} {
			name := option.name + "/nil flags"
			if withFlags {
				name = option.name + "/unchanged flags"
			}
			t.Run(name, func(t *testing.T) {
				isolateConfig(t)
				raw, expected := sampleOptionValue(option, false)
				t.Setenv(option.env, raw)
				var flags *pflag.FlagSet
				if withFlags {
					flags = NewFlagSet()
					if err := flags.Parse(nil); err != nil {
						t.Fatal(err)
					}
				}
				if err := LoadConfig(flags); err != nil {
					t.Fatal(err)
				}
				want := newDefaultSettings()
				reflect.ValueOf(&want).Elem().Field(option.index).Set(reflect.ValueOf(expected))
				if !reflect.DeepEqual(AppSettings, want) {
					t.Fatalf("%s did not override defaults: got %#v, want %#v", option.env, AppSettings, want)
				}
			})
		}
	}
}

func TestCLIPrecedenceForEveryOption(t *testing.T) {
	for _, option := range settingsOptions {
		t.Run(option.name, func(t *testing.T) {
			isolateConfig(t)
			env, _ := sampleOptionValue(option, false)
			raw, expected := sampleOptionValue(option, true)
			t.Setenv(option.env, env)
			flags := NewFlagSet()
			if err := flags.Parse([]string{"--" + option.name + "=" + raw}); err != nil {
				t.Fatal(err)
			}
			if err := LoadConfig(flags); err != nil {
				t.Fatal(err)
			}
			want := newDefaultSettings()
			reflect.ValueOf(&want).Elem().Field(option.index).Set(reflect.ValueOf(expected))
			if !reflect.DeepEqual(AppSettings, want) {
				t.Fatalf("CLI did not override ENV: got %#v, want %#v", AppSettings, want)
			}
		})
	}
}

func TestCLIExplicitZeroValues(t *testing.T) {
	isolateConfig(t)
	for env, value := range map[string]string{
		"HOST": "env-host", "PORT": "9001", "DEBUG": "true",
		"BANGUMI_USE_PROXY": "true", "PROXY": "http://env-proxy",
		"REFERENCE_PRIORITY": "bangumi",
	} {
		t.Setenv(env, value)
	}
	flags := NewFlagSet()
	if err := flags.Parse([]string{
		"--host=", "--port=0", "--debug=false", "--bangumi-use-proxy=false",
		"--proxy=", "--reference-priority=",
	}); err != nil {
		t.Fatal(err)
	}
	if err := LoadConfig(flags); err != nil {
		t.Fatal(err)
	}
	want := newDefaultSettings()
	want.Host, want.Port = "", 0
	if !reflect.DeepEqual(AppSettings, want) {
		t.Fatalf("explicit zero values lost: got %#v, want %#v", AppSettings, want)
	}
}

func TestCLIOverridesInvalidEnvironment(t *testing.T) {
	isolateConfig(t)
	t.Setenv("PORT", "not-an-integer")
	t.Setenv("DEBUG", "not-a-boolean")
	t.Setenv("REFERENCE_PRIORITY", "tvdb,tvdb")
	flags := NewFlagSet()
	if err := flags.Parse([]string{"--port=9000", "--debug=false", "--reference-priority=bangumi"}); err != nil {
		t.Fatal(err)
	}
	if err := LoadConfig(flags); err != nil {
		t.Fatalf("overridden invalid ENV should not be validated: %v", err)
	}
	if AppSettings.Port != 9000 || AppSettings.Debug || !reflect.DeepEqual(AppSettings.ReferencePriority, []string{"bangumi"}) {
		t.Fatalf("unexpected settings: %#v", AppSettings)
	}
}

func TestLoadFailurePreservesSettings(t *testing.T) {
	cases := []struct{ env, option, raw string }{
		{"PORT", "port", "invalid-secret-input"},
		{"PORT", "port", "1.5"},
		{"PORT", "port", "999999999999999999999999999999999999"},
		{"PORT", "port", " "},
		{"DEBUG", "debug", "invalid-secret-input"},
		{"BANGUMI_USE_PROXY", "bangumi-use-proxy", "invalid-secret-input"},
		{"LOG_MAX_SIZE_MIB", "log-max-size-mib", "0"},
		{"LOG_MAX_SIZE_MIB", "log-max-size-mib", "-1"},
		{"LOG_MAX_SIZE_MIB", "log-max-size-mib", "8796093022208"},
		{"LOG_MAX_FILES", "log-max-files", "0"},
		{"LOG_MAX_FILES", "log-max-files", "-1"},
		{"REFERENCE_PRIORITY", "reference-priority", "curetmdb,tvdb"},
		{"REFERENCE_PRIORITY", "reference-priority", "anilist"},
		{"REFERENCE_PRIORITY", "reference-priority", "tvdb,tvdb"},
		{"REFERENCE_PRIORITY", "reference-priority", "tvdb,"},
	}
	for _, tc := range cases {
		t.Run(tc.env+"/"+tc.raw, func(t *testing.T) {
			isolateConfig(t)
			AppSettings.Host = "keep-current-host"
			AppSettings.ReferencePriority = []string{"bangumi"}
			before := AppSettings
			t.Setenv("HOST", "do-not-commit-partial-settings")
			t.Setenv(tc.env, tc.raw)
			err := LoadConfig(nil)
			if err == nil || !strings.Contains(err.Error(), tc.env) || !strings.Contains(err.Error(), "--"+tc.option) {
				t.Fatalf("expected error naming the option and ENV, got %v", err)
			}
			if strings.Contains(err.Error(), "invalid-secret-input") {
				t.Fatalf("type error exposed the raw input: %v", err)
			}
			if !reflect.DeepEqual(AppSettings, before) {
				t.Fatalf("failed load changed settings: %#v", AppSettings)
			}
		})
	}
}

func TestInvalidCLIValues(t *testing.T) {
	for _, argument := range []string{"--port=invalid", "--port=999999999999999999999999999999", "--debug=invalid", "--bangumi-use-proxy=invalid"} {
		t.Run(argument, func(t *testing.T) {
			isolateConfig(t)
			before := AppSettings
			if err := NewFlagSet().Parse([]string{argument}); err == nil {
				t.Fatal("expected a parse error")
			}
			if !reflect.DeepEqual(AppSettings, before) {
				t.Fatal("CLI parsing changed application settings")
			}
		})
	}
	for _, raw := range []string{"anilist", "tvdb,tvdb", "curetmdb", "tvdb,"} {
		t.Run("reference/"+raw, func(t *testing.T) {
			isolateConfig(t)
			flags := NewFlagSet()
			if err := flags.Parse([]string{"--reference-priority=" + raw}); err != nil {
				t.Fatal(err)
			}
			before := AppSettings
			if err := LoadConfig(flags); err == nil {
				t.Fatal("expected a validation error")
			}
			if !reflect.DeepEqual(AppSettings, before) {
				t.Fatal("invalid references changed application settings")
			}
		})
	}
}

func TestDefaultListsAreIndependent(t *testing.T) {
	if reflect.ValueOf(AppSettings.ReferencePriority).Pointer() == reflect.ValueOf(DefaultSettings.ReferencePriority).Pointer() {
		t.Fatal("initial AppSettings shares the default list")
	}
	isolateConfig(t)
	first, second := newDefaultSettings(), newDefaultSettings()
	first.ReferencePriority[0] = "modified"
	if second.ReferencePriority[0] != "tvdb" || DefaultSettings.ReferencePriority[0] != "tvdb" {
		t.Fatal("constructing defaults shared a list")
	}
	if err := LoadConfig(nil); err != nil {
		t.Fatal(err)
	}
	previous := AppSettings.ReferencePriority
	previous[0] = "modified"
	if DefaultSettings.ReferencePriority[0] != "tvdb" {
		t.Fatal("loaded settings shared the default list")
	}
	if err := LoadConfig(nil); err != nil {
		t.Fatal(err)
	}
	if AppSettings.ReferencePriority[0] != "tvdb" || previous[0] != "modified" {
		t.Fatal("reloading reused the previous list")
	}
}

func TestFlagsHaveDefaultsUsageAndNoSecrets(t *testing.T) {
	isolateConfig(t)
	flags := NewFlagSet()
	help := flags.FlagUsages()
	for _, option := range settingsOptions {
		flag := flags.Lookup(option.name)
		if flag == nil || flag.Usage != option.usage || flag.Changed {
			t.Fatalf("invalid flag metadata for %s", option.name)
		}
		if !strings.Contains(help, "--"+option.name) || !strings.Contains(help, option.usage) {
			t.Fatalf("help missing option or usage for %s", option.name)
		}
		defaultValue := optionValueString(option.defaultValue)
		if option.sensitive && defaultValue != "" {
			if strings.Contains(help, defaultValue) || strings.Contains(flag.DefValue, defaultValue) {
				t.Fatalf("help exposed the default for %s", option.name)
			}
		} else if flag.DefValue != defaultValue {
			t.Fatalf("%s help default = %q, want %q", option.name, flag.DefValue, defaultValue)
		}
	}
	if key, err := flags.GetString("tvdb-api-key"); err != nil || key != DefaultSettings.TVDBAPIKey {
		t.Fatal("hiding help changed the actual default API key")
	}
	if !strings.Contains(help, "8632") || !strings.Contains(help, "tvdb,bangumi") {
		t.Fatal("help missing real defaults")
	}
	if err := flags.Parse([]string{"--debug", "--host=custom", "--reference-priority=bangumi"}); err != nil {
		t.Fatal(err)
	}
	other := NewFlagSet()
	if host, err := other.GetString("host"); err != nil || host != DefaultSettings.Host {
		t.Fatal("flag sets share state")
	}
	if err := LoadConfig(flags); err != nil || !AppSettings.Debug {
		t.Fatalf("bare boolean flag failed: %v", err)
	}
}

func TestNormalizeAndValidate(t *testing.T) {
	for _, references := range [][]string{nil, {}, {" TVDB ", " Bangumi "}} {
		settings := newDefaultSettings()
		settings.ReferencePriority = references
		if err := settings.NormalizeAndValidate(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(settings.ReferencePriority, DefaultSettings.ReferencePriority) {
			t.Fatalf("unexpected normalized references: %#v", settings.ReferencePriority)
		}
	}
}
