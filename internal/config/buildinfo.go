package config

var Version = "dev"

// BuiltinTVDBAPIKey 由官方构建通过 -ldflags -X 注入；源码构建默认为空。
var BuiltinTVDBAPIKey string
