package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestInitialBuiltinTVDBAPIKey(t *testing.T) {
	want := strings.TrimSpace(BuiltinTVDBAPIKey)
	if DefaultSettings.TVDBAPIKey != want || AppSettings.TVDBAPIKey != want {
		t.Fatal("initial settings did not use the build-time API key")
	}
	field, ok := reflect.TypeFor[Settings]().FieldByName("TVDBAPIKey")
	if !ok || field.Tag.Get("default") != "" {
		t.Fatal("the source API key default must be empty")
	}
}

func TestBuiltinTVDBAPIKeyPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		builtin string
		env     string
		args    []string
		want    string
	}{
		{name: "source build without key"},
		{name: "source build with environment", env: "env-key", want: "env-key"},
		{name: "builtin default", builtin: "builtin-test-key", want: "builtin-test-key"},
		{name: "trim builtin", builtin: " builtin-test-key\n", want: "builtin-test-key"},
		{name: "environment overrides builtin", builtin: "builtin-test-key", env: "env-key", want: "env-key"},
		{name: "cli overrides environment and builtin", builtin: "builtin-test-key", env: "env-key", args: []string{"--tvdb-api-key=cli-key"}, want: "cli-key"},
		{name: "cli disables builtin", builtin: "builtin-test-key", env: "env-key", args: []string{"--tvdb-api-key="}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldBuiltin := BuiltinTVDBAPIKey
			t.Cleanup(func() { BuiltinTVDBAPIKey = oldBuiltin })
			BuiltinTVDBAPIKey = tc.builtin
			isolateConfig(t)
			t.Setenv("TVDB_API_KEY", tc.env)
			flags := NewFlagSet()
			if err := flags.Parse(tc.args); err != nil {
				t.Fatal(err)
			}
			if err := LoadConfig(flags); err != nil {
				t.Fatal(err)
			}
			if AppSettings.TVDBAPIKey != tc.want {
				t.Fatal("unexpected API key configuration precedence")
			}
		})
	}
}

func TestBuiltinTVDBAPIKeyHiddenFromHelp(t *testing.T) {
	oldBuiltin := BuiltinTVDBAPIKey
	t.Cleanup(func() { BuiltinTVDBAPIKey = oldBuiltin })
	BuiltinTVDBAPIKey = "builtin-key-must-not-appear-in-help"
	isolateConfig(t)
	flags := NewFlagSet()
	if strings.Contains(flags.FlagUsages(), BuiltinTVDBAPIKey) || strings.Contains(flags.Lookup("tvdb-api-key").DefValue, BuiltinTVDBAPIKey) {
		t.Fatal("help exposed the build-time API key")
	}
	if key, err := flags.GetString("tvdb-api-key"); err != nil || key != BuiltinTVDBAPIKey {
		t.Fatal("hiding the key changed the flag default")
	}
	if err := LoadConfig(flags); err != nil {
		t.Fatal(err)
	}
	if AppSettings.TVDBAPIKey != BuiltinTVDBAPIKey {
		t.Fatal("hiding the key changed the loaded default")
	}
}
