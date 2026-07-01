package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/stansat/proby/internal/config"
	"github.com/stansat/proby/internal/i18n"
	"github.com/stansat/proby/internal/netinfo"
	"github.com/stansat/proby/internal/store"
)

func testServer(quit func()) *Server {
	st := store.New(config.GamingThresholds{
		MaxRTT: config.Duration(60 * time.Millisecond), MaxJitter: config.Duration(10 * time.Millisecond), MaxLoss: 0.02,
	})
	st.Register("cf", "1.1.1.1", "1.1.1.1", 0)
	st.AddPing("cf", store.PingSample{T: time.Now(), RTT: 5 * time.Millisecond, OK: true})
	ni := &netinfo.NetInfo{Hostname: "h", LocalMAC: "aa:bb:cc:dd:ee:ff", EgressIP: "10.0.0.5",
		LinkType: netinfo.LinkWired, Statuses: map[string]netinfo.Status{}}
	return New(Deps{
		Cfg:      &config.Config{Web: config.WebConfig{Listen: "127.0.0.1:0", WebSocket: true}, Targets: []config.Target{{Name: "cf", Host: "1.1.1.1"}}},
		Tr:       i18n.New(i18n.EN),
		Instance: "test",
		Version:  "v",
		Store:    st,
		NetInfo:  func() *netinfo.NetInfo { return ni },
		Quit:     quit,
	})
}

func TestIsLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:1234": true, "[::1]:80": true,
		"10.0.0.1:1234": false, "192.0.2.1:9": false,
	}
	for addr, want := range cases {
		r := &http.Request{RemoteAddr: addr}
		if got := isLoopback(r); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestQuitGating(t *testing.T) {
	quitCh := make(chan struct{}, 1)
	s := testServer(func() { quitCh <- struct{}{} })

	// Non-loopback POST -> 403.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/quit", nil)
	req.RemoteAddr = "10.1.2.3:5000"
	s.requireLoopback(s.handleQuit)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-loopback quit = %d, want 403", rec.Code)
	}

	// Spoofed forwarded header must not help.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/quit", nil)
	req.RemoteAddr = "10.1.2.3:5000"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	s.requireLoopback(s.handleQuit)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("spoofed XFF quit = %d, want 403", rec.Code)
	}

	// Loopback POST -> 200 and eventually triggers quit.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/quit", nil)
	req.RemoteAddr = "127.0.0.1:5000"
	s.requireLoopback(s.handleQuit)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("loopback quit = %d, want 200", rec.Code)
	}
	select {
	case <-quitCh:
	case <-time.After(time.Second):
		t.Fatal("quit callback was not invoked")
	}
}

func TestNetInfoSanitizedForNonLoopback(t *testing.T) {
	s := testServer(nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/netinfo", nil)
	req.RemoteAddr = "10.9.9.9:100"
	s.handleNetInfo(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "aa:bb:cc:dd:ee:ff") || strings.Contains(body, "10.0.0.5") {
		t.Errorf("SECURITY: sanitised netinfo leaked local detail: %s", body)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/netinfo", nil)
	req.RemoteAddr = "127.0.0.1:100"
	s.handleNetInfo(rec, req)
	if !strings.Contains(rec.Body.String(), "aa:bb:cc:dd:ee:ff") {
		t.Errorf("loopback netinfo should include full detail: %s", rec.Body.String())
	}
}

func TestWebSocketStream(t *testing.T) {
	s := testServer(nil)
	srv := httptest.NewServer(s.mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/stream"
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer c.CloseNow()

	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	var msg struct {
		Type    string              `json:"type"`
		Targets []store.TargetStats `json:"targets"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("ws json: %v", err)
	}
	if msg.Type != "snapshot" || len(msg.Targets) != 1 || msg.Targets[0].Name != "cf" {
		t.Errorf("unexpected snapshot: %+v", msg)
	}
}
