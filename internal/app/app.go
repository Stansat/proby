// Package app wires together the proby run flow: config, detection, connectivity,
// and the daemon. Command entrypoints live here so main stays thin.
package app

import (
	"fmt"
	"os"

	"github.com/stansat/proby/internal/config"
	"github.com/stansat/proby/internal/i18n"
)

// Options are the command-line inputs shared by every subcommand.
type Options struct {
	ConfigPath string
	Instance   string
	Lang       string
}

// env bundles the resolved runtime environment for a command.
type env struct {
	cfg      *config.Config
	tr       *i18n.Translator
	instance string
}

// setup performs step 0: load config, resolve language and instance label.
func setup(opts Options) (*env, error) {
	path := opts.ConfigPath
	if path == "" {
		path = config.DefaultPath
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}

	langStr := cfg.Language
	if opts.Lang != "" {
		langStr = opts.Lang
	}
	tr := i18n.New(i18n.Resolve(langStr))

	instance, fellBack := config.ResolveInstance(cfg, opts.Instance)
	if fellBack {
		fmt.Fprintf(os.Stderr, "warning: no instance configured; using hostname %q\n", instance)
	}
	for _, warn := range cfg.Lint() {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warn)
	}

	return &env{cfg: cfg, tr: tr, instance: instance}, nil
}

// setupOrExit is a helper that prints a config error and returns an exit code on failure.
func setupOrExit(opts Options) (*env, int) {
	e, err := setup(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return nil, 1
	}
	fmt.Fprintln(os.Stderr, e.tr.T(i18n.MsgConfigLoaded, configPathOf(opts)))
	return e, 0
}

func configPathOf(opts Options) string {
	if opts.ConfigPath != "" {
		return opts.ConfigPath
	}
	return config.DefaultPath
}
