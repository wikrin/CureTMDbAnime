package config

import (
	"reflect"
	"testing"
)

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
