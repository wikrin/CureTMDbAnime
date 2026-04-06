package cli

import (
	"flag"
	"fmt"
	"os"
)

func Execute(args []string, version string) (handled bool, err error) {
	fs := flag.NewFlagSet("curetmdbanime", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	showVersion := fs.Bool("version", false, "print version")
	fs.BoolVar(showVersion, "v", false, "print version")

	if err := fs.Parse(args); err != nil {
		return true, err
	}

	if !*showVersion {
		return false, nil
	}

	if version == "" {
		version = "dev"
	}

	if _, err := fmt.Fprintln(os.Stdout, version); err != nil {
		return true, err
	}

	return true, nil
}
