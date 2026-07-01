package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultPath is the config file used when none is given on the command line.
const DefaultPath = "proby.yml"

// Load reads, parses, merges defaults and validates the config at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	return Parse(data)
}

// Parse builds a Config from raw YAML bytes.
func Parse(data []byte) (*Config, error) {
	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if err := cfg.resolveTargets(); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// resolveTargets merges each target's ping/traceroute overrides onto copies of the
// defaults by decoding the raw YAML node on top of the default value.
func (c *Config) resolveTargets() error {
	for i := range c.Targets {
		t := &c.Targets[i]

		ping := c.Defaults.Ping
		if !t.PingNode.IsZero() {
			if err := t.PingNode.Decode(&ping); err != nil {
				return fmt.Errorf("target %q ping: %w", t.displayName(), err)
			}
		}
		t.Ping = ping

		tr := c.Defaults.Traceroute
		if !t.TraceNode.IsZero() {
			if err := t.TraceNode.Decode(&tr); err != nil {
				return fmt.Errorf("target %q traceroute: %w", t.displayName(), err)
			}
		}
		t.Traceroute = tr
	}
	return nil
}

func (t *Target) displayName() string {
	if t.Name != "" {
		return t.Name
	}
	return t.Host
}

// DisplayName returns the target's label (name, or host if unnamed).
func (t *Target) DisplayName() string { return t.displayName() }

// ResolveInstance determines the instance label with precedence:
// flag > config > OS hostname. It returns the resolved value and whether a
// hostname fallback was used (so the caller can warn).
func ResolveInstance(cfg *Config, flagValue string) (string, bool) {
	if v := sanitizeInstance(flagValue); v != "" {
		return v, false
	}
	if v := sanitizeInstance(cfg.Instance); v != "" {
		return v, false
	}
	host, _ := os.Hostname()
	if v := sanitizeInstance(host); v != "" {
		return v, true
	}
	return "proby", true
}

// sanitizeInstance restricts the value to a safe Prometheus label charset.
func sanitizeInstance(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '.' || r == ':' || r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('_')
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
