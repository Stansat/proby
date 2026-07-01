package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/stansat/proby/internal/netinfo"
)

// reportEnvelope is the JSON body POSTed to report_url.
type reportEnvelope struct {
	Instance string           `json:"instance"`
	Version  string           `json:"version"`
	NetInfo  *netinfo.NetInfo `json:"netinfo"`
}

// postReportURL sends the step-1 snapshot to report_url (secondary channel). It is a
// no-op if report_url is unset and soft-fails on error.
func postReportURL(ctx context.Context, e *env, ni *netinfo.NetInfo) {
	url := e.cfg.ReportURL
	if url == "" {
		return
	}
	body, err := json.Marshal(reportEnvelope{Instance: e.instance, Version: versionString(), NetInfo: ni})
	if err != nil {
		fmt.Fprintf(os.Stderr, "report_url: marshal failed: %v\n", err)
		return
	}
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "report_url: bad request: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "report_url: POST %s failed: %v\n", url, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "report_url: %s returned %s\n", url, resp.Status)
		return
	}
	fmt.Fprintf(os.Stderr, "report_url: snapshot posted to %s\n", url)
}
