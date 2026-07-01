package metrics

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/push"

	"github.com/stansat/proby/internal/config"
)

// authRoundTripper injects pushgateway credentials into each request.
type authRoundTripper struct {
	base  http.RoundTripper
	basic *config.BasicAuth
	cf    *config.CloudflareAccess
}

func (a *authRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if a.basic != nil {
		req.SetBasicAuth(a.basic.Username, a.basic.Password)
	}
	if a.cf != nil {
		req.Header.Set("CF-Access-Client-Id", a.cf.ClientID)
		req.Header.Set("CF-Access-Client-Secret", a.cf.ClientSecret)
	}
	return a.base.RoundTrip(req)
}

// RunPush pushes metrics to the configured pushgateway on its interval until ctx is
// cancelled. It is a no-op if no pushgateway is configured.
func (m *Metrics) RunPush(ctx context.Context) {
	pc := m.cfg.Pushgateway
	if pc == nil {
		return
	}
	interval := pc.Interval.Std()
	if interval <= 0 {
		interval = 15 * time.Second
	}
	pusher := m.newPusher(pc)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Final best-effort push on shutdown.
			m.pushOnce(pusher)
			return
		case <-ticker.C:
			m.pushOnce(pusher)
		}
	}
}

func (m *Metrics) newPusher(pc *config.PushgatewayConfig) *push.Pusher {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: pc.TLSInsecureSkipVerify},
	}
	timeout := pc.Timeout.Std()
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: &authRoundTripper{
			base:  transport,
			basic: pc.Auth.Basic,
			cf:    pc.Auth.CloudflareAccess,
		},
	}
	job := pc.Job
	if job == "" {
		job = "proby"
	}
	// The pushgateway groups (and overwrites) by grouping key. Without the
	// instance in the key, every probe pushing under the same job would clobber
	// the previous one's group, leaving only the last writer's metrics. Adding
	// the instance keeps each probe as a distinct group. It matches the instance
	// label already set on every series (same value), so the pushgateway accepts
	// it without conflict.
	return push.New(pc.URL, job).
		Grouping("instance", m.instance).
		Gatherer(m.pushGatherer()).
		Client(client)
}

// PushNow performs a single synchronous push, used on shutdown to flush final metrics
// before the process exits. It is a no-op if no pushgateway is configured.
func (m *Metrics) PushNow() {
	if m.cfg.Pushgateway == nil {
		return
	}
	m.pushOnce(m.newPusher(m.cfg.Pushgateway))
}

func (m *Metrics) pushOnce(pusher *push.Pusher) {
	if err := pusher.Push(); err != nil {
		m.pushFailures.Inc()
		log.Printf("pushgateway push failed: %v", err)
		return
	}
	m.pushSuccess.SetToCurrentTime()
}
