package config

import (
	"reflect"
	"testing"
)

func TestLoadConfigTVDBAPIKeyEnvironmentOverridesBuildValue(t *testing.T) {
	originalBuildValue := TVDBAPIKey
	originalSettings := AppSettings
	defer func() {
		TVDBAPIKey = originalBuildValue
		AppSettings = originalSettings
	}()

	TVDBAPIKey = "build-time-key"
	t.Setenv(EnvKeyTVDBAPIKey, "environment-key")

	if err := LoadConfig(nil); err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if AppSettings.TVDBAPIKey != "environment-key" {
		t.Fatalf("TVDBAPIKey = %q, want environment-key", AppSettings.TVDBAPIKey)
	}
}

func TestParseSeasonProviderPriority(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{name: "default", raw: "", want: []string{"tvdb", "bangumi"}},
		{name: "trim and lowercase", raw: " TVDB, Bangumi ", want: []string{"tvdb", "bangumi"}},
		{name: "curetmdb is fixed", raw: "curetmdb,tvdb", wantErr: true},
		{name: "unknown provider", raw: "anilist", wantErr: true},
		{name: "duplicate provider", raw: "tvdb,tvdb", wantErr: true},
		{name: "empty item", raw: "tvdb,", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSeasonProviderPriority(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSeasonProviderPriority(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseSeasonProviderPriority(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
		})
	}
}
