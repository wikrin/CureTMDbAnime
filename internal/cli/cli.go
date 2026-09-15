package cli

import (
	"fmt"
	"os"

	"curetmdbanime/internal/config"

	"github.com/spf13/pflag"
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
	fs := config.NewFlagSet()
	fs.SetOutput(os.Stderr)
	return fs
}
