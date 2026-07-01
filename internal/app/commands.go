package app

import (
	"fmt"
	"os"

	"github.com/stansat/proby/internal/connectivity"
	"github.com/stansat/proby/internal/i18n"
	"github.com/stansat/proby/internal/icmp"
	"github.com/stansat/proby/internal/netinfo"
	"github.com/stansat/proby/internal/report"
	"github.com/stansat/proby/internal/version"
)

// RunCheck runs step 2 (connectivity) only and returns 0 on pass, 1 on fail.
// Fully implemented in M1.
func RunCheck(opts Options) int {
	e, code := setupOrExit(opts)
	if code != 0 {
		return code
	}
	return runCheck(e)
}

// RunDiag runs detection + connectivity and prints the diagnostic report.
// Fully implemented in M2.
func RunDiag(opts Options) int {
	e, code := setupOrExit(opts)
	if code != 0 {
		return code
	}
	return runDiag(e)
}

// Run executes the full flow and starts the daemon.
// Fully implemented across M3-M7.
func Run(opts Options) int {
	e, code := setupOrExit(opts)
	if code != 0 {
		return code
	}
	return runDaemon(e)
}

// The following are milestone placeholders, replaced as features land.

func runCheck(e *env) int {
	conn, ok := openICMP(e)
	if !ok {
		return 1
	}
	defer conn.Close()

	res := checkConnectivity(e, conn)
	if res.Passed {
		return 0
	}
	return 1
}

// checkConnectivity runs step 2 and prints per-host results and a pass/fail summary.
func checkConnectivity(e *env, conn icmp.Conn) connectivity.Result {
	hosts := e.cfg.Connectivity.CheckHosts
	timeout := e.cfg.Connectivity.Timeout.Std()
	count := e.cfg.Connectivity.Count

	fmt.Fprintln(os.Stderr, e.tr.T(i18n.MsgConnCheckStart, len(hosts)))
	res := connectivity.Check(conn, hosts, count, timeout)

	reachable := 0
	for _, h := range res.Hosts {
		if h.OK() {
			reachable++
			fmt.Fprintln(os.Stderr, e.tr.T(i18n.MsgConnCheckHostOK, h.Host, h.Received, h.Sent, h.AvgRTT.Round(1e5)))
		} else {
			reason := "no reply"
			if h.Err != nil {
				reason = h.Err.Error()
			}
			fmt.Fprintln(os.Stderr, e.tr.T(i18n.MsgConnCheckHostFail, h.Host, reason))
		}
	}

	if res.Passed {
		fmt.Fprintln(os.Stderr, e.tr.T(i18n.MsgConnCheckPass, reachable, len(hosts)))
	} else {
		fmt.Fprintln(os.Stderr, e.tr.T(i18n.MsgConnCheckFail, len(hosts)))
	}
	return res
}

func runDiag(e *env) int {
	// Step 1: detect (soft-fail). Use ICMP if available to populate the gateway MAC.
	conn, haveICMP := openICMP(e)
	if haveICMP {
		defer conn.Close()
	}
	ni := collectNetInfo(e, conn)

	// Step 2: connectivity (only if ICMP is available).
	var connResult *connectivity.Result
	if haveICMP {
		r := checkConnectivity(e, conn)
		connResult = &r
	}

	fmt.Print(report.Render(ni, connResult, e.tr, versionString()))
	return 0
}

// collectNetInfo runs step-1 detection, passing the ICMP conn (may be nil) so the
// gateway MAC can be populated.
func collectNetInfo(e *env, conn icmp.Conn) *netinfo.NetInfo {
	return netinfo.Collect(netinfo.Options{Conn: conn})
}

func versionString() string {
	return fmt.Sprintf("%s (commit %s, built %s)", version.Version, version.Commit, version.Date)
}

func runDaemon(e *env) int {
	// Step 1: detect local network (soft-fail).
	conn, haveICMP := openICMP(e)
	if !haveICMP {
		// ICMP is required to do anything useful; explain and continue so the daemon
		// (web UI) still comes up, but the connectivity gate cannot run.
		fmt.Fprintln(os.Stderr, "ICMP unavailable; connectivity check and probing are disabled.")
	}
	if haveICMP {
		defer conn.Close()
	}
	ni := collectNetInfo(e, conn)

	// Step 2: connectivity gate. On total failure, print report and exit 1.
	if haveICMP {
		res := checkConnectivity(e, conn)
		if !res.Passed {
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, e.tr.T(i18n.MsgNoConnectivity))
			fmt.Fprintln(os.Stderr)
			fmt.Print(report.Render(ni, &res, e.tr, versionString()))
			return 1
		}
	}

	// Step 3: start the daemon (prober + metrics + web UI).
	fmt.Fprintf(os.Stderr, "%s\n", e.tr.T(i18n.MsgDaemonStart, e.instance))
	ctx, cancel := rootContext()
	defer cancel()

	d := startDaemon(ctx, e, ni, conn, cancel)

	// Secondary channel: POST the step-1 snapshot to report_url if configured.
	go postReportURL(ctx, e, ni)

	<-ctx.Done()
	fmt.Fprintln(os.Stderr, e.tr.T(i18n.MsgDaemonStop))
	d.Shutdown()
	return 0
}
