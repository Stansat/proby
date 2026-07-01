package config

import (
	"fmt"
	"strings"
)

// Warnings is a list of non-fatal configuration issues.
type Warnings []string

// Validate checks the config for fatal errors. Non-fatal issues are returned by Lint.
func (c *Config) Validate() error {
	if len(c.Targets) == 0 && len(c.Connectivity.CheckHosts) == 0 {
		return fmt.Errorf("config must define at least one target or connectivity.check_hosts")
	}
	for i, t := range c.Targets {
		if strings.TrimSpace(t.Host) == "" {
			return fmt.Errorf("target #%d has an empty host", i+1)
		}
	}
	if c.Web.Enabled && strings.TrimSpace(c.Web.Listen) == "" {
		return fmt.Errorf("web.enabled is true but web.listen is empty")
	}
	if c.Pushgateway != nil {
		p := c.Pushgateway
		if strings.TrimSpace(p.URL) == "" {
			return fmt.Errorf("pushgateway block present but pushgateway.url is empty")
		}
		if p.Auth.Basic != nil && p.Auth.CloudflareAccess != nil &&
			basicSet(p.Auth.Basic) && cfSet(p.Auth.CloudflareAccess) {
			return fmt.Errorf("pushgateway.auth: set at most one of basic / cloudflare_access")
		}
	}
	return nil
}

// Lint returns non-fatal warnings the caller should surface to the user.
func (c *Config) Lint() Warnings {
	var w Warnings
	if c.Pushgateway != nil {
		p := c.Pushgateway
		if p.Auth.Enabled() && !strings.HasPrefix(strings.ToLower(p.URL), "https://") {
			w = append(w, "pushgateway credentials are set but the URL is not HTTPS; credentials will be sent in the clear")
		}
		if p.Interval.Std() <= 0 {
			w = append(w, "pushgateway.interval is not positive; using 15s")
			p.Interval = Duration(15e9)
		}
	}
	return w
}

func basicSet(b *BasicAuth) bool { return b.Username != "" || b.Password != "" }
func cfSet(c *CloudflareAccess) bool {
	return c.ClientID != "" || c.ClientSecret != ""
}
