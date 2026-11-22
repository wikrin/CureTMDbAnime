package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestDescribeNewOrdinaryOptions(t *testing.T) {
	type example struct {
		Text  string   `option:"new-text" default:"hello" usage:"text"`
		Count int      `option:"new-count" default:"42" usage:"count"`
		On    bool     `option:"new-on" default:"true" usage:"on"`
		List  []string `option:"new-list" default:" first, second " usage:"list"`
		Skip  float64  `option:"-"`
	}
	options, err := describeOptions(reflect.TypeFor[example]())
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 4 {
		t.Fatalf("got %d options, want 4", len(options))
	}
	var settings example
	flags := pflag.NewFlagSet("example", pflag.ContinueOnError)
	for _, option := range options {
		if option.env != strings.ToUpper(strings.ReplaceAll(option.name, "-", "_")) {
			t.Fatalf("unexpected ENV name: %s", option.env)
		}
		reflect.ValueOf(&settings).Elem().Field(option.index).Set(reflect.ValueOf(option.defaultValue))
		option.registerFlag(flags, option.defaultValue)
	}
	want := example{Text: "hello", Count: 42, On: true, List: []string{"first", "second"}}
	if !reflect.DeepEqual(settings, want) {
		t.Fatalf("got %#v, want %#v", settings, want)
	}
	if err := flags.Parse([]string{"--new-text=changed", "--new-count=0", "--new-on=false", "--new-list=a,b"}); err != nil {
		t.Fatal(err)
	}
	for _, option := range options {
		decoded, err := decodeOptionValue(option.typ, flags.Lookup(option.name).Value.String())
		if err != nil {
			t.Fatal(err)
		}
		reflect.ValueOf(&settings).Elem().Field(option.index).Set(reflect.ValueOf(decoded))
	}
	want = example{Text: "changed", List: []string{"a", "b"}}
	if !reflect.DeepEqual(settings, want) {
		t.Fatalf("got %#v, want %#v", settings, want)
	}
}

func TestInvalidOptionDefinitions(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
	}{
		{"not a struct", reflect.TypeFor[string]()},
		{"missing option", reflect.TypeFor[struct{ Value string }]()},
		{"uppercase option", reflect.TypeFor[struct {
			Value string `option:"VALUE" default:"" usage:"value"`
		}]()},
		{"unexported field", reflect.TypeFor[struct {
			value string `option:"value" default:"" usage:"value"`
		}]()},
		{"missing default", reflect.TypeFor[struct {
			Value string `option:"value" usage:"value"`
		}]()},
		{"missing usage", reflect.TypeFor[struct {
			Value string `option:"value" default:""`
		}]()},
		{"unsupported scalar", reflect.TypeFor[struct {
			Value float64 `option:"value" default:"1" usage:"value"`
		}]()},
		{"unsupported list", reflect.TypeFor[struct {
			Value []int `option:"value" default:"1,2" usage:"value"`
		}]()},
		{"invalid integer", reflect.TypeFor[struct {
			Value int `option:"value" default:"not-an-integer" usage:"value"`
		}]()},
		{"invalid boolean", reflect.TypeFor[struct {
			Value bool `option:"value" default:"not-a-boolean" usage:"value"`
		}]()},
		{"invalid sensitive tag", reflect.TypeFor[struct {
			Value string `option:"value" default:"" usage:"value" sensitive:"invalid"`
		}]()},
		{"duplicate option", reflect.TypeFor[struct {
			First  string `option:"value" default:"" usage:"first"`
			Second string `option:"value" default:"" usage:"second"`
		}]()},
		{"duplicate ENV", reflect.TypeFor[struct {
			First  string `option:"foo-bar" default:"" usage:"first"`
			Second string `option:"foo_bar" default:"" usage:"second"`
		}]()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := describeOptions(tc.typ); err == nil {
				t.Fatal("expected an invalid definition error")
			}
		})
	}
}

func TestDecodeOptionValue(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
		raw  any
		want any
	}{
		{"string", reflect.TypeFor[string](), " unchanged ", " unchanged "},
		{"integer", reflect.TypeFor[int](), " 12 ", 12},
		{"typed integer", reflect.TypeFor[int](), 0, 0},
		{"boolean", reflect.TypeFor[bool](), " TRUE ", true},
		{"typed boolean", reflect.TypeFor[bool](), false, false},
		{"list", reflect.TypeFor[[]string](), " a, b ", []string{"a", "b"}},
		{"list with empty item", reflect.TypeFor[[]string](), "a,", []string{"a", ""}},
		{"empty list", reflect.TypeFor[[]string](), " ", []string(nil)},
		{"typed list", reflect.TypeFor[[]string](), []string{"a", "b"}, []string{"a", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeOptionValue(tc.typ, tc.raw)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, %v; want %#v", got, err, tc.want)
			}
		})
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[int](), reflect.TypeFor[bool](), reflect.TypeFor[[]string]()} {
		if _, err := decodeOptionValue(typ, struct{}{}); err == nil {
			t.Fatalf("unexpectedly accepted invalid value for %s", typ)
		}
	}
	original := []string{"a"}
	decoded, err := decodeOptionValue(reflect.TypeFor[[]string](), original)
	if err != nil {
		t.Fatal(err)
	}
	decoded.([]string)[0] = "changed"
	if original[0] != "a" {
		t.Fatal("typed list was not cloned")
	}
}
