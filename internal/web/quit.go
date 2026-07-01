package web

import (
	"log"
	"net"
	"net/http"
	"time"
)

// isLoopback reports whether the request's TCP peer address is a loopback address.
// It deliberately uses RemoteAddr (the real connection) and ignores forwarded headers.
func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// requireLoopback wraps a handler so only loopback callers may reach it.
func (s *Server) requireLoopback(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isLoopback(r) {
			log.Printf("rejected non-loopback access to %s from %s", r.URL.Path, r.RemoteAddr)
			http.Error(w, "forbidden: localhost only", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// handleQuit triggers a graceful shutdown. Guarded by requireLoopback and POST-only.
func (s *Server) handleQuit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed: use POST", http.StatusMethodNotAllowed)
		return
	}
	log.Println(s.deps.Tr.Lang(), "quit requested from local UI")
	writeJSON(w, map[string]string{"status": "shutting down"})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	// Let the response flush before cancelling the root context.
	go func() {
		time.Sleep(150 * time.Millisecond)
		if s.deps.Quit != nil {
			s.deps.Quit()
		}
	}()
}
