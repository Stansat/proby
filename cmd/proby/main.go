// Command proby is a remote-deployed network probe: it detects local network
// configuration, verifies connectivity, then runs a prober daemon with a web UI and
// Prometheus metrics.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/stansat/proby/internal/app"
	"github.com/stansat/proby/internal/version"
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		configPath = flag.String("config", "", "path to proby.yml (default ./proby.yml)")
		instance   = flag.String("instance", "", "override the instance metric label")
		lang       = flag.String("lang", "", "force UI language: en | pl")
	)
	flag.StringVar(configPath, "c", "", "shorthand for --config")
	flag.Usage = usage
	flag.Parse()

	opts := app.Options{
		ConfigPath: *configPath,
		Instance:   *instance,
		Lang:       *lang,
	}

	cmd := "run"
	if args := flag.Args(); len(args) > 0 {
		cmd = args[0]
	}

	switch cmd {
	case "version":
		fmt.Printf("proby %s (commit %s, built %s, %s)\n",
			version.Version, version.Commit, version.Date, version.GoVersion())
		return 0
	case "check":
		return app.RunCheck(opts)
	case "diag":
		return app.RunDiag(opts)
	case "validate":
		return app.RunValidate(opts)
	case "run":
		return app.Run(opts)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `proby - remote network probe

Usage:
  proby [flags] [command]

Commands:
  run       run the full flow and start the daemon (default)
  check     run the connectivity check only, exit 0/1
  diag      run detection + connectivity, print the diagnostic report, exit
  validate  load + validate the config offline (no network), exit 0/1
  version   print version information

Flags:
`)
	flag.PrintDefaults()
}
