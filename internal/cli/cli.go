package cli

import (
	"fmt"
	"os"

	"github.com/spf13/pflag"
)

const (
	flagHost            = "host"
	flagPort            = "port"
	flagDebug           = "debug"
	flagTmdbAPIURL      = "tmdb-api-url"
	flagCureSource      = "cure-source"
	flagProxy           = "proxy"
	flagDataDir         = "data-dir"
	flagBangumiAPIURL   = "bangumi-api-url"
	flagBangumiUseProxy = "bangumi-use-proxy"
)

// 保存 CLI 解析结果
type Result struct {
	ConfigFlags *pflag.FlagSet
}

// 处理版本参数并解析配置类参数
func Execute(args []string, version string) (Result, bool, error) {
	configFlags := NewConfigFlagSet()
	showVersion := configFlags.BoolP("version", "v", false, "print version")

	if err := configFlags.Parse(args); err != nil {
		return Result{}, false, err
	}

	if *showVersion {
		if version == "" {
			version = "dev"
		}

		if _, err := fmt.Fprintln(os.Stdout, version); err != nil {
			return Result{}, true, err
		}

		return Result{}, true, nil
	}

	return Result{ConfigFlags: configFlags}, false, nil
}

// 创建配置类参数集合
func NewConfigFlagSet() *pflag.FlagSet {
	fs := pflag.NewFlagSet("config", pflag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	fs.String(flagHost, "", "服务监听地址")
	fs.Int(flagPort, 0, "服务监听端口")
	fs.Bool(flagDebug, false, "启用调试模式")
	fs.String(flagTmdbAPIURL, "", "TMDB API URL")
	fs.String(flagCureSource, "", "CureTMDb 数据源 URL")
	fs.String(flagProxy, "", "HTTP/HTTPS 代理地址 (例如: http://127.0.0.1:7890)")
	fs.String(flagDataDir, "", "数据存储目录")
	fs.String(flagBangumiAPIURL, "", "Bangumi API URL")
	fs.Bool(flagBangumiUseProxy, true, "Bangumi API 请求是否使用 PROXY 代理")

	return fs
}
