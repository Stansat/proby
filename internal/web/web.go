// Package web serves the proby web UI, JSON API, WebSocket live feed, the local
// /metrics endpoint and a localhost-only quit endpoint.
package web

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"time"

	"github.com/stansat/proby/internal/config"
	"github.com/stansat/proby/internal/hoststat"
	"github.com/stansat/proby/internal/i18n"
	"github.com/stansat/proby/internal/netinfo"
	"github.com/stansat/proby/internal/store"
)

//go:embed assets/*
var assetsFS embed.FS

// Deps are the dependencies the web server needs.
type Deps struct {
	Cfg            *config.Config
	Tr             *i18n.Translator
	Instance       string
	Version        string
	Store          *store.Store
	MetricsHandler http.Handler
	NetInfo        func() *netinfo.NetInfo
	HostStat       func() *hoststat.Stats // may be nil
	Quit           func()                 // triggers graceful shutdown
}

// Server is the proby HTTP server.
type Server struct {
	deps    Deps
	mux     *http.ServeMux
	httpSrv *http.Server
	hub     *hub
}

// New builds the server and registers all routes.
func New(d Deps) *Server {
	s := &Server{deps: d, mux: http.NewServeMux(), hub: newHub()}
	s.httpSrv = &http.Server{Addr: d.Cfg.Web.Listen, Handler: s.mux}
	s.routes()
	return s
}

func (s *Server) routes() {
	// Static UI.
	sub, _ := fs.Sub(assetsFS, "assets")
	s.mux.Handle("/", http.FileServer(http.FS(sub)))

	// Metrics + health.
	if s.deps.MetricsHandler != nil {
		s.mux.Handle("/metrics", s.deps.MetricsHandler)
	}
	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	})

	// JSON API.
	s.mux.HandleFunc("/api/meta", s.handleMeta)
	s.mux.HandleFunc("/api/targets", s.handleTargets)
	s.mux.HandleFunc("/api/targets/", s.handleTargetDetail)
	s.mux.HandleFunc("/api/netinfo", s.handleNetInfo)
	s.mux.HandleFunc("/api/hoststat", s.handleHostStat)
	s.mux.HandleFunc("/api/report", s.handleReport)
	s.mux.HandleFunc("/api/stream", s.handleStream)
	s.mux.HandleFunc("/api/quit", s.requireLoopback(s.handleQuit))
}

// Run starts the server and the live broadcaster, blocking until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	go s.broadcaster(ctx)
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutCtx)
	}()

	fmt.Fprintln(os.Stderr, s.deps.Tr.T(i18n.MsgWebListening, s.deps.Cfg.Web.Listen))
	if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
