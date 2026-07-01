# proby — Implementation Plan

A single-binary remote network probe. Delivered to a customer as **two files**:

1. `proby` / `proby.exe` — a self-contained binary (no runtime deps, assets embedded).
2. `proby.yml` — a small human-editable config file.

The probe detects local network configuration, verifies connectivity, then runs a
long-lived daemon that continuously pings/traceroutes a small set of targets and
exposes a web UI plus Prometheus metrics.

---

## 1. Goals & Constraints

| Area | Decision |
|------|----------|
| Language | Go (single static binary, `CGO_ENABLED=0` where possible) |
| Platforms | Windows + Linux now; macOS later. All code paths behind a platform abstraction. |
| Delivery | 2 files only: binary + `proby.yml`. All web assets embedded via `embed`. |
| UI languages | Polish + English (i18n), selectable in config (`auto` by default). |
| ICMP | Own ping + traceroute engine; use vetted low-level modules, not a full turnkey pinger. |
| Targets | Small count (typically 1–3, max ~10). Efficiency is *not* the priority; correctness, clarity, and portability are. |
| Dev / test | Develop on Windows; validate Linux builds via Docker Desktop. |
| Failure posture | Step 1 (local detection) **soft-fails** — never aborts. Step 2 (connectivity) hard-fails only if *all* hosts fail. |

**Non-goals (v1):** authentication on the web UI, TLS termination, packet capture,
active bandwidth testing, config hot-reload.

---

## 2. Run Flow (spec restated as the authoritative behaviour)

```
0. Load config
   - Default path: ./proby.yml
   - Override:     proby --config /path/to/proby.yml   (also -c)
   - Validate + apply defaults. Fatal error only if the file is unreadable/invalid.

1. Detect local network config  (ALL steps soft-fail, run in any order, collect partial data)
   - default route
   - IP address used to reach the default gateway
   - link type: wifi vs wired (best-effort)
   - local MAC (of the egress interface)
   - gateway MAC (from ARP/neighbour table; ping gateway first to populate it)
   - full routing table
   - full ARP / neighbour table
   -> Each item carries an OK / Unavailable(reason) status; nothing here aborts the program.

2. Connectivity check
   - ICMP echo to connectivity.check_hosts from config.
   - PASS if AT LEAST ONE host replies.
   - FAIL only if EVERY host fails:
       * print a verbose, human-readable failure explanation
       * print a nicely formatted diagnostic report built from step 1
         (so the user can copy/paste it to their network operator)
       * exit non-zero.

3. Start the daemon
   - Start the prober (per-target ping + traceroute schedulers).
   - Start the host-stats collector (interface counters, WiFi signal, host resource pressure).
   - Open the on-disk history ring file (append-only) and replay recent samples into the store.
   - Start the web UI (+ WebSocket live feed) + local /metrics endpoint.
   - Start the pushgateway pusher (PRIMARY export): every `interval` push all metrics
     INCLUDING the cyclically-refreshed environment snapshot (step-1 data as metrics).
   - Optionally also POST the step-1 JSON to config.report_url if set (secondary; soft-fail).
   - Run until SIGINT/SIGTERM (Ctrl+C), then shut down gracefully (flush history, final push).
```

---

## 3. Architecture

### 3.1 Project layout

```
proby/
├── cmd/proby/
│   └── main.go                 # CLI entrypoint, flag parsing, orchestration of the run flow
├── internal/
│   ├── config/                 # YAML load, schema, defaults, validation
│   │   ├── config.go
│   │   └── defaults.go
│   ├── i18n/                   # message catalogs (pl, en), locale detection
│   │   ├── i18n.go
│   │   └── messages/{en,pl}.go
│   ├── netinfo/                # STEP 1 — local network detection (platform-split)
│   │   ├── netinfo.go          # exported types + Collect() facade + soft-fail wrapping
│   │   ├── netinfo_linux.go
│   │   ├── netinfo_windows.go
│   │   └── netinfo_darwin.go   # stubs first, filled when macOS is prioritised
│   ├── connectivity/           # STEP 2 — check + decision logic
│   │   └── connectivity.go
│   ├── report/                 # formatted diagnostic report (text + optional JSON)
│   │   └── report.go
│   ├── icmp/                   # OWN ping + traceroute engine (platform-split socket layer)
│   │   ├── ping.go
│   │   ├── traceroute.go
│   │   ├── session.go          # id/seq allocation, matching replies to probes
│   │   ├── socket_posix.go     # //go:build linux || darwin
│   │   └── socket_windows.go   # IcmpSendEcho2 path
│   ├── prober/                 # scheduling, per-target workers, lifecycle
│   │   ├── prober.go
│   │   └── target.go
│   ├── hoststat/               # continuous host metrics (platform-split)
│   │   ├── hoststat.go         # iface counters, WiFi signal, CPU/mem pressure
│   │   ├── hoststat_linux.go
│   │   ├── hoststat_windows.go
│   │   └── hoststat_darwin.go
│   ├── store/                  # in-memory ring buffers + rollups (percentiles, jitter, MOS)
│   │   └── store.go
│   ├── history/                # append-only ring file (on-disk sample persistence)
│   │   └── history.go
│   ├── metrics/                # Prometheus collectors + pushgateway client
│   │   └── metrics.go
│   ├── web/                    # HTTP server, JSON API, WebSocket, embedded UI
│   │   ├── web.go
│   │   ├── api.go
│   │   ├── ws.go               # WebSocket live-update hub
│   │   ├── assets/             # go:embed — html/css/js (no external CDN)
│   │   └── assets.go
│   └── version/
│       └── version.go          # build info (version, commit, date) via -ldflags
├── proby.example.yml
├── Dockerfile                  # linux build/test image
├── Makefile / Taskfile.yml     # build matrix, lint, test
├── .goreleaser.yaml            # cross-compiled release artifacts
├── PLAN.md
└── IDEAS.md
```

### 3.2 High-level data flow

```
config ──► netinfo (refreshed cyclically) ──► NetInfo snapshot ─┬─► report (on fail / on demand)
                                                                ├─► proby_env_* metrics (push-only)
                                                                └─► optional POST to report_url (secondary)

prober   ──► per target: PingWorker  ──► samples ─► store ─┬─► web API (JSON) ─► browser UI
                         TraceWorker  ──► samples ─► store ─┤   └─► WebSocket live push ─► UI
hoststat ──► iface counters / WiFi RSSI / CPU-mem ─► store ─┤
                                                store ──────┼─► base registry ──► local /metrics (filtered)
                       base + env registries ───────────────┼─► PUSH (auth) ──► Pushgateway  ★primary
                                                store ──────┴─► history (append-only ring file)
                                                history ─────► replay on startup ─► store
```

### 3.3 Concurrency model

- One `context.Context` rooted at `main`, cancelled on signal → clean shutdown of all workers.
- Per target: one goroutine for ping (ticker `ping.interval`), one for traceroute
  (ticker `traceroute.interval`). Workers write into a thread-safe `store`.
- One `hoststat` goroutine samples host metrics on its own ticker (default `5s`).
- `store` uses per-target ring buffers guarded by an `RWMutex`; web/metrics read snapshots.
  It maintains streaming rollups (percentiles, jitter, MOS) so reads are cheap.
- One `history` goroutine drains a buffered channel of samples and appends them to the
  on-disk ring file; the write path never blocks probe workers (drop-oldest on backpressure).
- The web `ws` hub fans store updates out to connected browsers over WebSocket.
- ICMP receive: a single receiver goroutine per socket demultiplexes replies by
  (id, seq) to the waiting probe (avoids one raw socket per probe). Windows
  `IcmpSendEcho2` is call/response so it needs no shared receiver.

---

## 4. Configuration (`proby.yml`)

Parsed with `gopkg.in/yaml.v3`. Unknown keys warn but don't fail. Durations use Go
syntax (`1s`, `500ms`, `10s`).

```yaml
# Deployment identifier. Attached as the `instance` label to EVERY metric so multiple
# proby deployments are distinguishable in Prometheus. Overridable with --instance.
instance: "site-warsaw-01"

# Language for CLI output and web UI: auto | en | pl
language: auto

# Optional SECONDARY one-shot JSON intake for the step-1 snapshot. The PRIMARY way
# environment data reaches the operator is now as metrics via the pushgateway (§10.2);
# this HTTP POST is kept only for setups that want a plain JSON drop. Omit to skip.
report_url: ""            # e.g. https://ops.example.com/proby/intake

web:
  enabled: true
  listen: "0.0.0.0:8080"  # bind address:port for the UI + /metrics
  websocket: true         # live-push updates to the browser (falls back to polling if off)
  metrics_include_environment: false  # if true, expose sensitive proby_env_* on local /metrics too

connectivity:
  # Step 2 uses these. PASS if ANY replies; FAIL only if ALL fail.
  check_hosts: ["1.1.1.1", "8.8.8.8", "9.9.9.9"]
  count: 3                # echoes per host during the check
  timeout: "2s"

# Prometheus Pushgateway — the PRIMARY metrics export path. Omit the block to disable
# pushing (the local /metrics endpoint stays available regardless).
pushgateway:
  url: "https://pushgw.example.com"   # base URL (https strongly recommended)
  job: "proby"
  interval: "15s"                     # push cadence for ALL metrics incl. environment
  include_environment: true           # push the step-1 env snapshot as metrics (see §10.2)
  timeout: "10s"
  tls_insecure_skip_verify: false     # only for self-signed internal gateways
  # Credentials — pick AT MOST ONE scheme. Omit both for an unauthenticated gateway.
  auth:
    # (a) HTTP Basic auth
    basic:
      username: ""
      password: ""
    # (b) Cloudflare Access service token (sends CF-Access-Client-Id / -Secret headers)
    cloudflare_access:
      client_id: ""
      client_secret: ""

# On-disk sample persistence (append-only ring file). Omit to keep in-memory only.
history:
  enabled: true
  path: "proby.history"   # single ring file next to the binary
  max_size: "64MB"        # ring wraps oldest-first when full
  flush_interval: "5s"    # batch fsync cadence

# Continuous host metrics collector.
host_stats:
  enabled: true
  interval: "5s"
  interface_counters: true   # bytes/packets/errors/drops on the egress iface
  wifi: true                 # RSSI/SNR/link-rate/channel (SSID redacted by default)
  resource_pressure: true    # CPU load + memory pressure for context

# Defaults applied to every target unless overridden per-target.
defaults:
  ping:
    interval: "1s"
    timeout: "2s"
    payload_size: 56
    dscp: 0                # DSCP/ToS marking (0=default; e.g. 46=EF for VoIP path tests)
  traceroute:
    interval: "10s"
    max_hops: 30
    timeout: "2s"
    queries: 3            # probes per hop
    protocol: icmp        # v1: icmp only (udp/tcp = future, see IDEAS.md)
    dscp: 0
  # Thresholds for the "gaming ready" verdict shown in the UI (latency/jitter/loss).
  quality:
    gaming_ready:
      max_rtt: "60ms"
      max_jitter: "10ms"
      max_loss: 0.02       # 2%

# Monitored targets. Per-target ping/traceroute blocks override defaults.
targets:
  - name: "Cloudflare DNS"
    host: "1.1.1.1"
  - name: "Google DNS"
    host: "8.8.8.8"
    ping:       { interval: "2s", dscp: 46 }
    traceroute: { interval: "30s", max_hops: 20 }
  - name: "Customer gateway"
    host: "example.customer.com"   # hostnames allowed; resolved once at start, re-resolved on failure
```

### Config responsibilities
- Merge `defaults` into each target (deep-merge; per-target wins).
- Resolve hostnames to IPs at startup; keep the original string for display/labels.
- Validate: at least one target OR connectivity hosts present; port in `web.listen`
  is free-ish (bind attempted lazily with a clear error).
- **`instance` resolution** (precedence): `--instance` flag > `instance:` in config >
  OS hostname as a last-resort fallback (logged as a warning, since a stable configured
  id is strongly preferred for consistent Prometheus series). The resolved value is
  validated to a safe label charset (`[A-Za-z0-9_.:-]`, spaces→`_`) and frozen for the
  process lifetime.

---

## 5. Step 1 — Local network detection (`internal/netinfo`)

Exported snapshot type; every field independently populated and independently
fallible. A collection error on one field never blocks the others.

```go
type Status struct { OK bool; Reason string }   // Reason set when !OK

type NetInfo struct {
    DefaultRoute   Route            // gateway IP, egress iface, metric
    EgressIP       net.IP           // source IP toward the gateway
    LinkType       LinkType         // Wired | WiFi | Unknown
    LocalMAC       net.HardwareAddr
    GatewayMAC     net.HardwareAddr
    RoutingTable   []Route
    ARPTable       []ARPEntry
    Hostname       string
    Interfaces     []IfaceSummary
    // per-field status:
    Statuses       map[string]Status
    CollectedAt    time.Time
}
```

### Detection strategy per field

| Field | Linux | Windows | macOS (later) |
|-------|-------|---------|---------------|
| default route + routing table | `netlink` (RTM_GETROUTE) with `/proc/net/route` fallback | `iphlpapi.GetIpForwardTable2` via `golang.org/x/sys/windows` | `route -n get default` / `sysctl net.route` |
| egress IP | `net.Dial("udp", gateway:0)` then read LocalAddr (no packets sent) | same trick | same |
| link type (wifi/wired) | `/sys/class/net/<if>/wireless` exists → WiFi; else check `IFF_*`/driver | `GetAdaptersAddresses` → `IfType == IF_TYPE_IEEE80211` | `networksetup -listallhardwareports` / IOKit |
| local MAC | `net.InterfaceByName(egress).HardwareAddr` | same | same |
| gateway MAC | ping gateway to populate, then read `/proc/net/arp` / `netlink NEIGH` | ping gateway, then `GetIpNetTable2` | `arp -an` |
| ARP table | `/proc/net/arp` or netlink neighbours | `GetIpNetTable2` | `arp -an` |

**Portability rule:** prefer native syscalls via `golang.org/x/sys` and
`github.com/vishvananda/netlink` (Linux) over shelling out. Where a syscall path is
too costly for v1, a documented command-parsing fallback is acceptable *only* inside
the platform file, never in shared code. Candidate helper libs:
`github.com/jackpal/gateway` (default gateway), `github.com/libp2p/go-netroute`
(route lookup) — evaluate, vendor only if they reduce platform code meaningfully.

**Privileges:** reading tables needs no elevation. Populating the gateway MAC needs
an ICMP echo (see §7 privilege notes). If ICMP is unavailable, gateway MAC just
reports `Unavailable("could not populate neighbour cache")`.

### 5.1 Continuous host stats (`internal/hoststat`)

Distinct from the one-shot step-1 detection: this collector runs *throughout* the daemon
on `host_stats.interval`, feeding time-series into the store/metrics/UI. Same soft-fail
discipline — any unavailable metric is skipped, never fatal. All platform-split.

| Metric group | Linux | Windows | macOS (later) |
|--------------|-------|---------|---------------|
| **interface counters** (rx/tx bytes, packets, errors, drops, link speed) | `/sys/class/net/<if>/statistics/*` or netlink | `GetIfEntry2` / `GetIfTable2` (iphlpapi) | `sysctl net.link` / `getifaddrs` |
| **WiFi signal** (RSSI, SNR, link rate, channel; SSID/BSSID gathered but redacted for metrics) | `nl80211` via netlink (or `iw` fallback) | `WlanQueryInterface` (wlanapi) | CoreWLAN / `airport -I` |
| **resource pressure** (CPU load %, memory used %) | `/proc/stat`, `/proc/meminfo` | `GetSystemTimes`, `GlobalMemoryStatusEx` | `host_statistics` / `sysctl` |

- Kept deliberately light (a handful of syscalls every few seconds). `gopsutil` is an
  option but pulls a lot in; prefer direct reads confined to the platform files.
- WiFi signal is the highest-value item for flaky wireless customer sites — surfaced in
  the UI as an RSSI bar and correlated on the same timeline as latency/loss.
- SSID/BSSID may be collected for the on-screen local-info panel but are **never** placed
  in `/metrics` labels (see §10 security filter).

---

## 6. Step 2 — Connectivity check + report (`internal/connectivity`, `internal/report`)

- Send `connectivity.count` echoes to each `check_hosts` entry with `connectivity.timeout`.
- Aggregate: `passed = any host with ≥1 reply`.
- **On pass:** log a short success line, continue to daemon.
- **On fail (all hosts dead):**
  1. Print a verbose i18n message ("No connectivity: all N check hosts failed…").
  2. Render the diagnostic report from the step-1 `NetInfo` snapshot.
  3. Exit code `1`.

### Report format (`report.Render`)
- Plain-text, monospace-friendly, boxed sections, copy-paste ready.
- Sections: timestamp + proby version, default route, egress IP + link type,
  local/gateway MAC, per-check-host results, routing table, ARP table, unavailable
  items with reasons.
- Also available at runtime via the web UI ("Copy diagnostic report" button) and a
  CLI subcommand `proby diag` that runs steps 1–2 and prints the report without
  starting the daemon.

---

## 7. ICMP engine (`internal/icmp`) — our own ping & traceroute

Built on `golang.org/x/net/icmp`, `.../ipv4`, `.../ipv6`. We implement matching,
sequencing, timeouts, TTL stepping, and stats ourselves.

### 7.1 Ping
- Echo Request with our `id` (per-process) and incrementing `seq`; payload of
  `payload_size` bytes with an embedded send-timestamp for RTT.
- Single receiver goroutine per socket demuxes Echo Replies to waiting probes.
- Records: sent, received, RTT samples, loss%, jitter (see §8 for the exact jitter/MOS math).

### 7.2 Traceroute (MTR-style)
- For hop = 1..max_hops: send `queries` probes with IP TTL = hop.
- Collect ICMP **Time Exceeded** (intermediate hop address + RTT) and **Echo Reply**
  (destination reached → stop).
- Continuous mode: each traceroute cycle updates per-hop rolling stats
  (best/avg/worst/last RTT, loss, stddev) — the classic `mtr` table.
- Reverse-DNS each hop address (cached, async, best-effort).

### 7.3 DSCP / ToS marking
- Each probe can set the IP DSCP/ToS byte (`ping.dscp` / `traceroute.dscp`, 0–63).
- Set via `ipv4.PacketConn.SetTOS` / `ipv6.PacketConn.SetTrafficClass` on POSIX, and the
  `IP_OPTION_INFORMATION.Tos` field on the Windows `IcmpSendEcho2` path.
- Enables testing QoS-marked paths (e.g. DSCP 46 / EF to validate the VoIP/gaming class
  end-to-end). Note: many carriers bleach DSCP — surface the marking used so results are
  interpretable. The `dscp` value flows through the `Socket.SendEcho` signature below.

### 7.4 Platform socket layer (the portability crux)

| Path | Mechanism | Privilege |
|------|-----------|-----------|
| **Linux (preferred)** | `net.ListenPacket("udp4","...")` → unprivileged ICMP datagram socket (`SOCK_DGRAM`, `IPPROTO_ICMP`); set TTL via `ipv4.PacketConn` for traceroute | none, if `net.ipv4.ping_group_range` allows; else raw socket needs `CAP_NET_RAW`/root |
| **Linux (fallback)** | raw `ip4:icmp` socket | root or `CAP_NET_RAW` (or `setcap cap_net_raw+ep proby`) |
| **Windows** | `iphlpapi` `IcmpSendEcho2` / `Icmp6SendEcho2` via `golang.org/x/sys/windows`; TTL through `IP_OPTION_INFORMATION` for traceroute | no admin needed for ICMP API (a big reason to use it over raw sockets) |
| **macOS (later)** | `SOCK_DGRAM`/`IPPROTO_ICMP` datagram sockets (supported) | none for ICMP datagram |

Behind interface:

```go
type Socket interface {
    SendEcho(dst net.IP, id, seq, ttl, dscp int, payload []byte) error
    Receive(timeout time.Duration) (Reply, error) // src, type (EchoReply|TimeExceeded), rtt
    Close() error
}
```

`socket_posix.go` (`//go:build linux || darwin`) and `socket_windows.go` provide it.
Shared `ping.go`/`traceroute.go` contain zero platform code.

**Startup probe:** on launch, the engine self-tests which mode works and logs it
(e.g. "ICMP: unprivileged datagram socket OK" / "requires CAP_NET_RAW"). If ICMP is
entirely unavailable, the daemon still starts, targets show `error: no ICMP
permission`, and the report explains the fix (setcap / run as admin).

---

## 8. Prober, store & history (`internal/prober`, `internal/store`, `internal/history`)

- `prober.Run(ctx, cfg, store, metrics)` spins up per-target workers.
- Ping worker: ticker at `ping.interval`; each tick sends one echo, records sample.
- Trace worker: ticker at `traceroute.interval`; each tick runs a full traceroute cycle.
- **store**: per-target ring buffers:
  - ping samples: `{t, rtt, ok}` (bounded window, e.g. last 3600 for a 1h graph).
  - traceroute: current hop table with rolling per-hop aggregates.
  - Derived rollups computed incrementally on each sample.

### 8.1 RTT percentiles & histograms
- Maintain a bounded-window RTT distribution per target. Two representations:
  - **UI/API percentiles**: p50/p90/p95/p99 over the display window (sorted slice or a
    lightweight streaming estimator like t-digest/P²; exact sort is fine at our scale).
  - **Prometheus histogram**: fixed buckets (e.g. 1,2,5,10,20,50,100,200,500,1000 ms) so
    the server side can compute quantiles across scrapes — see §10.

### 8.2 Jitter & MOS ("gaming ready")
- **Jitter**: RFC 3550 inter-arrival style — `J += (|D(i-1,i)| - J) / 16`, where `D` is the
  difference of consecutive RTTs. Also expose simple stddev of RTT for the graph.
- **MOS**: E-model–derived estimate from latency, jitter and loss:
  - effective latency `= rtt/2 + 2*jitter + codec_delay`
  - R-factor reduced by an effective-latency penalty and a loss penalty, then mapped to
    MOS (1.0–4.5). Documented constants; a rough-but-useful VoIP-style score.
- **Gaming-ready verdict**: boolean per target from `defaults.quality.gaming_ready`
  thresholds (max RTT, max jitter, max loss over the window). The UI renders a
  🎮 **"Gaming ready"** / ⚠️ **"Not ideal"** badge; the score is also exported as a metric.

### 8.3 Store outputs
- Store exposes read-only snapshots for web + metrics, and publishes update events to the
  WebSocket hub (§9) and to the history writer (§8.4).

### 8.4 On-disk history (`internal/history`) — append-only ring file
- Single fixed-size file (`history.path`, `history.max_size`). Append-only within a ring:
  when the write offset reaches the cap it wraps to the start (oldest records overwritten).
- Record framing: small fixed header (magic, version, ring head/tail offsets) + length-
  prefixed, CRC-checked binary sample records (target id, kind, timestamp, value(s)).
  Compact enough that 64 MB holds many hours of 1 s samples for a handful of targets.
- Batched writes with `flush_interval` fsync; a bounded channel decouples probe workers
  from disk (drop-oldest on backpressure so probing never stalls on I/O).
- **Startup replay**: on boot, scan the ring oldest→newest, validate CRCs, and refill the
  in-memory store so graphs and rollups survive restarts. Corrupt/torn tail records are
  skipped, not fatal. A format-version mismatch → start fresh (log a warning).

---

## 9. Web UI + API (`internal/web`)

- Stdlib `net/http` (+ lightweight router; `chi` optional). All assets `go:embed`ed.
- **Pages** (server-rendered shell + a small vanilla-JS/Chart-ish frontend, no CDN):
  - **Overview**: all targets, up/down, current RTT, loss%, sparkline, and per-target
    🎮 **"Gaming ready"** badge (green) / ⚠️ **"Not ideal"** (amber) from the §8.2 verdict.
  - **Target detail**: RTT time-series graph with p50/p95/p99 bands, loss over time,
    jitter + MOS score, and the MTR-style hop table (hop, host, loss%, sent,
    last/avg/best/worst/stddev), color-coded. DSCP marking in use shown per target.
  - **Local info**: the step-1 snapshot + **host stats** panel (interface counters,
    WiFi signal quality with an RSSI bar, CPU/memory pressure) + "Copy diagnostic report".
- **Live updates**: when `web.websocket` is on, the page subscribes to `GET /api/stream`
  (WebSocket) and the store's update hub pushes new samples/rollups → smooth real-time
  graphs. Falls back to polling (`?poll=1` / auto-detect) if the socket can't connect.
- **JSON API** (drives the UI, also machine-usable):
  - `GET /api/targets` — list + current status (incl. percentiles, jitter, MOS, gaming-ready).
  - `GET /api/targets/{name}/ping?window=…` — time-series.
  - `GET /api/targets/{name}/traceroute` — current hop table.
  - `GET /api/netinfo` — step-1 snapshot (sanitised).
  - `GET /api/hoststat` — current host stats (iface counters, WiFi, resource pressure; sanitised).
  - `GET /api/report` — rendered diagnostic report (text).
  - `GET /api/stream` — **WebSocket** live feed of store updates.
  - `POST /api/quit` — **localhost-only** graceful shutdown (see below).
  - `GET /healthz` — liveness.
- **Quit from the UI (localhost-only)**: a "Quit proby" button in the UI issues
  `POST /api/quit`, which triggers the same graceful shutdown as SIGINT (cancel root
  context → stop workers → final metrics push → flush history → exit 0).
  - **Access gate**: the handler is wrapped in `requireLoopback` middleware that allows
    the request only when the connection's remote address is loopback
    (`127.0.0.0/8`, `::1`) — checked from `http.Request.RemoteAddr` (the real TCP peer,
    **not** `X-Forwarded-For`/`Host`, so a proxy or spoofed header can't unlock it).
    Non-loopback callers get `403 Forbidden`; the button is also hidden in the served
    page when the request origin isn't loopback (defense-in-depth; the server check is
    authoritative). Rejected attempts are logged.
  - The endpoint is `POST`-only (no drive-by `GET`), and a small confirm step in the UI
    guards against accidental clicks.
- **WebSocket**: use `nhooyr.io/websocket` / `coder/websocket` (pure Go, small) or
  `gorilla/websocket`; a hub goroutine tracks clients and broadcasts JSON deltas.
- **i18n**: `Accept-Language` + `?lang=` + config override; strings from catalogs.

---

## 10. Metrics (`internal/metrics`) — Pushgateway (primary) + local `/metrics`

Prometheus client (`github.com/prometheus/client_golang`).

### 10.1 Two export paths & two registries

The **Pushgateway is the primary export path**; the local `/metrics` HTTP endpoint is
always kept as well (for on-site debugging / a local Prometheus scrape). Because these
two channels have very different trust levels, they are backed by **two registries**:

| Path | Trust | Contents |
|------|-------|----------|
| **Pushgateway push** (primary) | Authenticated outbound channel the customer configured to *their* operator | base metrics **+** the environment snapshot (§10.2), incl. sensitive detail (IP/MAC/route/ARP) when `include_environment: true` |
| **local `/metrics`** (secondary) | May be reachable on an untrusted LAN | base metrics **only** — the §10.3 security filter strips all sensitive local-machine detail |

Implementation: a `baseRegistry` (ping/traceroute/host/quality/build) is registered in
both. An `envRegistry` (the `proby_env_*` collectors) is added **only** to the push
gatherer by default; it appears on local `/metrics` only if the operator explicitly opts
in via `web.metrics_include_environment: true` (default false).

**Instance label:** every metric carries `instance="<resolved id>"` (see §4 precedence) so
deployments are distinguishable. Applied once, centrally, as registry-wide
`ConstLabels{"instance": …}` — collectors never repeat it and can't forget it. The push
client also sets it as the `instance` grouping key
(`push.New(url,job).Grouping("instance", id)`) so pushed series don't collide across
deployments. (Series below omit `instance` for brevity — assume it on all of them.)

**Pushgateway client & auth** (`internal/metrics` push loop):
- Built on `push.New(url, job)` with a custom `*http.Client` whose `RoundTripper` injects
  credentials from `pushgateway.auth` — **at most one** scheme:
  - `basic` → `Authorization: Basic …` (via `SetBasicAuth` / header).
  - `cloudflare_access` → `CF-Access-Client-Id` + `CF-Access-Client-Secret` headers
    (Cloudflare Access service token; the gateway sits behind CF Access).
- `timeout`, `tls_insecure_skip_verify` honoured on that client. Push failures are logged
  and retried on the next cycle (never fatal); a `proby_push_last_success_timestamp` and
  `proby_push_failures_total` are exposed locally so push health is itself observable.
- Config validation rejects setting both `basic` and `cloudflare_access`, and warns if
  credentials are set with a non-HTTPS `url`.

### 10.2 Environment snapshot as metrics (cyclic)

The step-1 local-network data is exported **as metrics, refreshed every push cycle**
(not a one-shot at boot). A `proby_env_*` collector re-reads a cached `netinfo` snapshot
that is refreshed on `pushgateway.interval` (capped to a sane minimum), so gateway/link/IP
changes show up over time. Modelled as info-style metrics (value `1`, detail in labels):

```
proby_env_info{hostname,egress_ip,egress_iface,link_type,local_mac,gateway_ip,gateway_mac} 1
proby_env_link_type{link_type}                 1     # wifi|wired|unknown (enum convenience)
proby_env_route{dst,gateway,iface,metric}      1     # one series per routing-table row
proby_env_arp{ip,mac,iface}                    1     # one series per ARP/neighbour entry
proby_env_default_route_present                gauge # 1/0
proby_env_collect_errors{field}                gauge # 1 per soft-failed field (with reason omitted)
```

Cardinality is bounded (tiny routing/ARP tables at customer sites). These series carry
sensitive detail and therefore ship **only via the pushgateway** by default (§10.1).

### 10.3 Series & security filter

Proposed series (labels in braces, `instance` implicit on every series):

```
# Build / process
proby_build_info{version,commit,goversion} 1
proby_up 1

# Ping
proby_ping_rtt_seconds{target,host}                 gauge (last)
proby_ping_rtt_seconds_bucket{target,host,le}       histogram   # fixed buckets → server-side quantiles
proby_ping_rtt_quantile_seconds{target,host,quantile} gauge     # p50/p90/p95/p99 over window (precomputed)
proby_ping_sent_total{target,host}                  counter
proby_ping_received_total{target,host}              counter
proby_ping_loss_ratio{target,host}                  gauge (0..1, rolling window)
proby_ping_jitter_seconds{target,host}              gauge       # RFC 3550 style (see §8.2)
proby_ping_dscp{target,host}                         gauge       # DSCP value in use for the probes
proby_quality_mos{target,host}                       gauge       # estimated MOS 1.0–4.5
proby_quality_gaming_ready{target,host}              gauge       # 1 = meets thresholds, 0 = not

# Traceroute
proby_traceroute_hops{target,host}                          gauge
proby_traceroute_hop_rtt_seconds{target,host,hop,hop_addr}  gauge
proby_traceroute_hop_loss_ratio{target,host,hop,hop_addr}   gauge

# Host stats (from internal/hoststat — NON-SENSITIVE VALUES ONLY, see filter below)
proby_iface_rx_bytes_total{iface}          counter
proby_iface_tx_bytes_total{iface}          counter
proby_iface_rx_errors_total{iface}         counter
proby_iface_tx_errors_total{iface}         counter
proby_iface_rx_dropped_total{iface}        counter
proby_iface_tx_dropped_total{iface}        counter
proby_iface_speed_bps{iface}               gauge
proby_wifi_signal_dbm{iface}               gauge     # RSSI
proby_wifi_link_rate_bps{iface}            gauge
proby_wifi_channel{iface}                  gauge     # SSID/BSSID NOT exported (see filter)
proby_host_cpu_load_ratio                  gauge     # 0..1 (context for slow probes)
proby_host_mem_used_ratio                  gauge     # 0..1

# Push health (local /metrics, so push status is observable even when pushes fail)
proby_push_last_success_timestamp          gauge
proby_push_failures_total                  counter

# Local machine — SAFE summary (base registry, appears on BOTH paths)
proby_network_info{link_type,gateway_present,egress_iface}  1   # no IP/MAC values here
proby_target_up{target,host}                                gauge

# (Sensitive environment detail lives in the proby_env_* series of §10.2 — push-only.)
```

**Security filter (`/metrics` path):** the base registry served at local `/metrics` must
NOT leak data that is a security risk. Rule: **no IP addresses, MAC addresses, local
hostname, ARP entries, routing tables, or WiFi SSID/BSSID in its metric labels.** Local
info there is reduced to booleans / enums (link type, "gateway present") and numeric host
stats (counters, RSSI, load); interface labels use the OS iface name only. Target hosts
*are* exposed (customer's own config, the point of the probe). This filtering is
centralised in the registry split (§10.1) so it can't be bypassed by adding a new metric.

**Why the pushgateway may carry more:** the `proby_env_*` sensitive series (§10.2) go out
only over the authenticated, customer-configured pushgateway channel to their own
operator — the exact recipient the diagnostic snapshot is meant for. They are excluded
from local `/metrics` unless `web.metrics_include_environment: true` is explicitly set.

---

## 11. i18n (`internal/i18n`)

- Simple catalog: `map[MessageID]string` per locale (`en`, `pl`); `T(id, args...)`.
- Locale resolution: `config.language` (`auto` → OS locale env `LANG`/Windows
  `GetUserDefaultLocaleName`, default `en`). Web UI can override per-request.
- All user-facing CLI/report/UI strings go through the catalog; no hardcoded prose in
  logic. Start with the strings the flow already needs (steps 0–3, report, UI labels).

---

## 12. CLI surface (`cmd/proby`)

```
proby                      # run full flow with ./proby.yml
proby -c /etc/proby.yml    # explicit config (also --config)
proby diag                 # run steps 1–2, print diagnostic report, exit (no daemon)
proby check                # run step 2 only, exit 0/1 (scriptable connectivity gate)
proby version              # print version/commit/build date
proby --lang pl ...        # force language
proby --instance site-01   # override the `instance` metric label (wins over config)
```

Flags parsed with stdlib `flag` (or `spf13/pflag` for `--long`). Subcommands kept
minimal.

---

## 13. Build, cross-compilation & delivery

- **Static binaries**, `CGO_ENABLED=0`. Assets embedded so a single file ships.
- Build matrix: `windows/amd64`, `windows/arm64`, `linux/amd64`, `linux/arm64`
  (+ `darwin/amd64`, `darwin/arm64` when macOS lands).
- Version via `-ldflags "-X .../version.Version=... -X .../version.Commit=..."`.
- **`.goreleaser.yaml`** produces the matrix, checksums, and a zip/tar containing
  exactly `proby[.exe]` + `proby.example.yml`.
- **Docker** (`Dockerfile`) for Linux build/test:
  - multi-stage: `golang:alpine` build → `scratch`/`distroless` runtime, or just used
    to run the binary for manual verification under Docker Desktop.
  - A `docker compose` file with a couple of test targets and network toys for
    reproducing failure scenarios.
- `Makefile`/`Taskfile`: `build`, `build-all`, `test`, `lint` (golangci-lint), `run`,
  `docker-linux`.

### Privilege packaging notes (documented in README)
- Linux: recommend `sudo setcap cap_net_raw+ep ./proby` OR run as root OR ensure
  `net.ipv4.ping_group_range` covers the user (then unprivileged datagram ICMP works).
- Windows: `IcmpSendEcho2` needs no admin; the web bind port must be permitted by the
  firewall (document the prompt).

---

## 14. Testing strategy

- **Unit**: config merge/defaults/validation (incl. rejecting dual pushgateway auth +
  HTTPS-with-creds warning); report rendering (golden files); ICMP packet build/parse
  (round-trip encode→decode, incl. DSCP byte); store rollup math (loss, jitter,
  percentiles, MOS, gaming-ready verdict); history ring write→wrap→replay round-trip
  incl. torn-tail recovery; **registry split** (assert `proby_env_*` and any IP/MAC/SSID
  NEVER appear on the local `/metrics` gatherer, but DO appear on the push gatherer);
  push auth RoundTripper injects the right headers for basic vs cloudflare_access;
  `requireLoopback` gate on `POST /api/quit` (loopback `RemoteAddr` → 200/shutdown;
  non-loopback, and spoofed `X-Forwarded-For`/`Host` → 403).
- **Platform-tagged tests**: netinfo parsers (feed captured `/proc/net/route`,
  `/proc/net/arp`, sample table bytes) so they run cross-platform in CI.
- **Integration (Linux/Docker)**: run the binary against `1.1.1.1`/containers; assert
  `/metrics`, `/api/*`, and traceroute produce sane data. Simulate "no connectivity"
  with an isolated docker network → assert step-2 fail path + report + exit 1.
- **Windows manual/CI**: `IcmpSendEcho2` path smoke test (needs real network).
- **Lint/vet**: `go vet`, `golangci-lint`, `govulncheck` in CI.
- CI matrix builds all targets to catch platform-file breakage early.

---

## 15. Milestones (incremental, each independently runnable)

**M0 — Skeleton**
`go.mod`, layout, `version`, `config` load+defaults, `i18n` scaffold, `proby version`.

**M1 — ICMP ping (cross-platform)**
`icmp` engine + `socket_posix`/`socket_windows`, `proby check` (step 2) working on
Windows & Linux. Startup privilege self-test.

**M2 — Netinfo + report**
`netinfo` for Linux + Windows (soft-fail), `report` renderer, `proby diag`. Wire the
full step 0→1→2 flow including the fail path (report + exit 1).

**M3 — Prober + store + traceroute**
Per-target ping/trace workers, ring-buffer store, MTR-style hop aggregation, DSCP
marking. Streaming rollups: percentiles, RFC-3550 jitter, MOS, gaming-ready verdict.

**M4 — Metrics + pushgateway (primary export)**
Prometheus collectors (incl. RTT histogram, jitter, MOS, gaming-ready); the two-registry
split (base vs env) + security filter on local `/metrics`; pushgateway push loop with
basic / cloudflare-access auth; cyclic `proby_env_*` environment metrics; push-health
metrics.

**M5 — Web UI**
Embedded assets, overview (with 🎮 gaming-ready badge) + target detail (percentile
bands, jitter/MOS) + local-info/host-stats pages, JSON API, **WebSocket live feed**,
i18n in UI, "copy diagnostic report".

**M6 — Host stats + on-disk history**
`hoststat` (interface counters, WiFi signal, CPU/mem) for Linux + Windows;
append-only ring `history` file with startup replay.

**M7 — Secondary report_url + polish**
Optional POST of step-1 snapshot to `report_url`; graceful shutdown (flush history, final
push); error UX; docs.

**M8 — Release engineering**
goreleaser matrix, Docker Linux validation, README (install, privileges, config),
2-file release artifact.

**M9 — macOS**
Fill `netinfo_darwin.go` + `hoststat_darwin.go`, verify ICMP datagram path, add darwin
to the build matrix.

---

## 16. Dependencies (candidate set, keep minimal)

| Purpose | Module |
|---------|--------|
| ICMP / IP TTL | `golang.org/x/net/{icmp,ipv4,ipv6}` |
| Syscalls (Win/Linux) | `golang.org/x/sys/{windows,unix}` |
| Linux routes/neighbours | `github.com/vishvananda/netlink` (eval) |
| Default gateway helper | `github.com/jackpal/gateway` (eval) |
| YAML | `gopkg.in/yaml.v3` |
| Prometheus | `github.com/prometheus/client_golang` |
| Router (optional) | `github.com/go-chi/chi/v5` |
| WebSocket | `github.com/coder/websocket` (pure Go) or `gorilla/websocket` |
| CLI flags (optional) | `github.com/spf13/pflag` |

Every dependency must build with `CGO_ENABLED=0` for all target OSes, or be confined
to a single platform file.

---

## 17. Open questions / decisions to confirm

1. **Web UI charts**: hand-rolled SVG/canvas vs a tiny embedded JS chart lib
   (uPlot ~40KB). Leaning uPlot, vendored/embedded (still one binary).
2. **IPv6**: build the engine dual-stack from the start (both `ipv4`/`ipv6`
   PacketConns), or ship IPv4 first? Proposal: structure for both, ship IPv4 in M1
   and enable IPv6 in M3.
3. **report_url auth**: does the intake endpoint need a token/header? Add
   `report_url_headers` map to config if so.
4. **Persistence**: now in v1 as an append-only ring file (§8.4). Open sub-question:
   fixed binary framing (proposed) vs a pure-Go embedded store (e.g. `bbolt`). Leaning
   custom ring for simplicity and predictable size.
5. **MOS model constants**: which E-model simplification/codec assumptions to hardcode
   for the MOS/gaming-ready score, and whether to expose them in config. Proposal: ship
   sane G.711-ish defaults; make `quality` thresholds configurable, constants internal.
6. **Windows service / Linux systemd**: run in foreground for v1; document
   `nssm`/systemd unit examples rather than embedding a service manager.
