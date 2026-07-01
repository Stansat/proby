package app

import (
	"fmt"
	"os"

	"github.com/stansat/proby/internal/i18n"
	"github.com/stansat/proby/internal/icmp"
)

// openICMP opens the ICMP connection and logs the mode (or the failure reason).
// It returns a nil Conn (and false) when ICMP is unavailable, so callers can decide
// whether that is fatal.
func openICMP(e *env) (icmp.Conn, bool) {
	conn, err := icmp.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, e.tr.T(i18n.MsgICMPUnavailable, err))
		return nil, false
	}
	fmt.Fprintln(os.Stderr, e.tr.T(i18n.MsgICMPMode, conn.Mode()))
	return conn, true
}
