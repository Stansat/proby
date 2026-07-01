package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/stansat/proby/internal/netinfo"
	"github.com/stansat/proby/internal/report"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// handleMeta returns UI metadata, including whether the caller may quit (loopback).
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	names := make([]string, 0, len(s.deps.Cfg.Targets))
	for _, t := range s.deps.Cfg.Targets {
		names = append(names, t.DisplayName())
	}
	writeJSON(w, map[string]any{
		"instance":  s.deps.Instance,
		"version":   s.deps.Version,
		"lang":      string(s.deps.Tr.Lang()),
		"websocket": s.deps.Cfg.Web.WebSocket,
		"can_quit":  isLoopback(r) && s.deps.Quit != nil,
		"loopback":  isLoopback(r),
		"targets":   names,
	})
}

// handleTargets returns the derived stats for all targets.
func (s *Server) handleTargets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.deps.Store.Snapshot())
}

// handleTargetDetail serves /api/targets/{name}/ping and /{name}/traceroute.
func (s *Server) handleTargetDetail(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/targets/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	name, sub := parts[0], parts[1]
	switch sub {
	case "ping":
		limit := 300
		series := s.deps.Store.PingSeries(name, limit)
		writeJSON(w, series)
	case "traceroute":
		st, ok := s.deps.Store.TargetSnapshot(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, st.Hops)
	default:
		http.NotFound(w, r)
	}
}

// sanitizedNetInfo is the reduced snapshot exposed to non-loopback callers.
type sanitizedNetInfo struct {
	Hostname            string    `json:"hostname"`
	LinkType            string    `json:"link_type"`
	EgressIface         string    `json:"egress_iface"`
	DefaultRoutePresent bool      `json:"default_route_present"`
	InterfaceCount      int       `json:"interface_count"`
	RouteCount          int       `json:"route_count"`
	ArpCount            int       `json:"arp_count"`
	CollectedAt         time.Time `json:"collected_at"`
}

// handleNetInfo returns the full snapshot to loopback callers and a sanitised summary
// (no IP/MAC/tables) to everyone else.
func (s *Server) handleNetInfo(w http.ResponseWriter, r *http.Request) {
	ni := s.currentNetInfo()
	if ni == nil {
		writeJSON(w, map[string]any{})
		return
	}
	if isLoopback(r) {
		writeJSON(w, ni)
		return
	}
	writeJSON(w, sanitizedNetInfo{
		Hostname:            ni.Hostname,
		LinkType:            string(ni.LinkType),
		EgressIface:         ni.EgressIface,
		DefaultRoutePresent: ni.DefaultGateway != "",
		InterfaceCount:      len(ni.Interfaces),
		RouteCount:          len(ni.RoutingTable),
		ArpCount:            len(ni.ARPTable),
		CollectedAt:         ni.CollectedAt,
	})
}

// handleReport renders the full diagnostic report; loopback-only.
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r) {
		http.Error(w, "forbidden: diagnostic report is available from localhost only", http.StatusForbidden)
		return
	}
	ni := s.currentNetInfo()
	if ni == nil {
		ni = &netinfo.NetInfo{Statuses: map[string]netinfo.Status{}}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(report.Render(ni, nil, s.deps.Tr, s.deps.Version)))
}

// handleHostStat is a placeholder until M6.
func (s *Server) handleHostStat(w http.ResponseWriter, r *http.Request) {
	if s.deps.HostStat == nil {
		writeJSON(w, map[string]any{})
		return
	}
	writeJSON(w, s.deps.HostStat())
}

func (s *Server) currentNetInfo() *netinfo.NetInfo {
	if s.deps.NetInfo == nil {
		return nil
	}
	return s.deps.NetInfo()
}
