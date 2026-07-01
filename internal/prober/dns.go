package prober

import (
	"net"
	"sync"
)

// dnsCache resolves PTR (reverse DNS) records for hop addresses, caching results and
// performing the lookup asynchronously so it never blocks a traceroute cycle.
type dnsCache struct {
	mu      sync.Mutex
	names   map[string]string
	pending map[string]bool
}

func newDNSCache() *dnsCache {
	return &dnsCache{names: map[string]string{}, pending: map[string]bool{}}
}

// lookup returns the cached hostname for ip, kicking off an async resolution on the
// first request. It returns "" until the name is known.
func (c *dnsCache) lookup(ip string) string {
	c.mu.Lock()
	if name, ok := c.names[ip]; ok {
		c.mu.Unlock()
		return name
	}
	if c.pending[ip] {
		c.mu.Unlock()
		return ""
	}
	c.pending[ip] = true
	c.mu.Unlock()

	go func() {
		name := ""
		if names, err := net.LookupAddr(ip); err == nil && len(names) > 0 {
			name = trimDot(names[0])
		}
		c.mu.Lock()
		c.names[ip] = name
		delete(c.pending, ip)
		c.mu.Unlock()
	}()
	return ""
}

func trimDot(s string) string {
	if n := len(s); n > 0 && s[n-1] == '.' {
		return s[:n-1]
	}
	return s
}
