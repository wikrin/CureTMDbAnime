package config

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestLogRetentionConfig(t *testing.T) {
	for _, tc := range []struct {
		argument string
		valid    bool
	}{
		{"--log-max-size-mib=0", false},
		{"--log-max-size-mib=-1", false},
		{"--log-max-files=0", false},
		{"--log-max-files=-1", false},
		{"--log-max-size-mib=1", true},
		{"--log-max-files=1", true},
	} {
		t.Run(tc.argument, func(t *testing.T) {
			isolateConfig(t)
			flags := NewFlagSet()
			if err := flags.Parse([]string{tc.argument}); err != nil {
				t.Fatal(err)
			}
			err := LoadConfig(flags)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, got %v", tc.valid, err)
			}
		})
	}
	if strconv.IntSize == 64 {
		settings := newDefaultSettings()
		tooLarge := int64(1 << 43)
		settings.LogMaxSizeMiB = int(tooLarge)
		if err := settings.NormalizeAndValidate(); err == nil || !strings.Contains(err.Error(), "LOG_MAX_SIZE_MIB") {
			t.Fatal("MiB to bytes overflow accepted")
		}
	}
}

func TestParseReferencePriority(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{name: "default", raw: "", want: []string{"tvdb", "bangumi"}},
		{name: "trim and lowercase", raw: " TVDB, Bangumi ", want: []string{"tvdb", "bangumi"}},
		{name: "curetmdb is fixed", raw: "curetmdb,tvdb", wantErr: true},
		{name: "unknown reference", raw: "anilist", wantErr: true},
		{name: "duplicate reference", raw: "tvdb,tvdb", wantErr: true},
		{name: "empty item", raw: "tvdb,", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseReferencePriority(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseReferencePriority(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ParseReferencePriority(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
		})
	}
}
