# proby

A single-binary, remote-deployed network probe. It detects the local network
configuration, verifies connectivity, then runs a daemon that continuously pings and
traceroutes a small set of targets — exposing a web UI and Prometheus metrics.

Ships as **two files**: the binary (`proby` / `proby.exe`) and a config file
(`proby.yml`). All web assets are embedded; there are no runtime dependencies.

- **Platforms:** Windows and Linux (macOS support is partial — see below).
- **UI languages:** English and Polish.
- **ICMP:** own ping + traceroute engine. No administrator rights required on Windows.

---

## Quick start

```bash
# 1. Put proby(.exe) and a proby.yml next to each other.
cp proby.example.yml proby.yml   # then edit targets, instance, pushgateway...

# 2. Run it.
./proby                      # full flow: detect -> connectivity gate -> daemon
```

Then open the web UI at `http://<host>:8080/` and metrics at `http://<host>:8080/metrics`.

### Commands

```
proby                     # run the full flow and start the daemon (default)
proby check               # connectivity check only; exit 0 (ok) / 1 (all failed)
proby diag                # detection + connectivity, print a diagnostic report, exit
proby version             # print version information

Flags:
  -c, --config PATH       # config file (default ./proby.yml)
      --instance NAME     # override the instance metric label
      --lang en|pl        # force UI language
```

---

## Run flow

0. **Load config** (`./proby.yml` or `--config`).
1. **Detect local network** (soft-fail — never aborts): default route, egress IP,
   wired/WiFi, local & gateway MAC, routing table, ARP table.
2. **Connectivity gate:** ICMP to `connectivity.check_hosts`. Passes if *any* host
   replies. If *all* fail, it prints a diagnostic report (for your network operator)
   and exits non-zero.
3. **Daemon:** per-target ping + traceroute, host-stats collection, on-disk history,
   web UI + `/metrics`, and (if configured) pushgateway export.

`proby diag` runs steps 1–2 and prints the report without starting the daemon — handy
to hand to a network operator.

---

## Privileges

ICMP is the only privileged operation.

- **Windows:** none. proby uses the `IcmpSendEcho` API, which needs no administrator
  rights. (The Windows firewall may prompt to allow the web listen port.)
- **Linux:** proby first tries an *unprivileged* ICMP datagram socket. If your system
  restricts it, either:
  - `sudo setcap cap_net_raw+ep ./proby`, or
  - run as root, or
  - widen `net.ipv4.ping_group_range` to include the user.

  If ICMP is unavailable, the daemon still starts (web UI comes up) but probing and the
  connectivity gate are disabled, and the startup log explains the fix.

---

## Configuration

See [`proby.example.yml`](proby.example.yml) for a fully-commented example. Highlights:

```yaml
instance: "site-warsaw-01"   # attached as the `instance` label to EVERY metric
language: auto               # auto | en | pl
monitor_gateway: true        # auto-add the detected default gateway as a target

web:
  listen: "0.0.0.0:8080"
  websocket: true
  metrics_include_environment: false   # keep sensitive env off local /metrics

connectivity:
  check_hosts: ["1.1.1.1", "8.8.8.8", "9.9.9.9"]

pushgateway:                 # PRIMARY metrics export (omit to disable)
  url: "https://pushgw.example.com"
  job: "proby"
  interval: "15s"
  include_environment: true
  auth:                      # pick AT MOST ONE
    basic: { username: "", password: "" }
    # cloudflare_access: { client_id: "", client_secret: "" }

defaults:
  ping:       { interval: "1s", timeout: "2s", payload_size: 56, dscp: 0 }
  traceroute: { interval: "10s", max_hops: 30, queries: 3, dscp: 0 }
  quality:
    gaming_ready: { max_rtt: "60ms", max_jitter: "10ms", max_loss: 0.02 }

targets:
  - { name: "Cloudflare DNS", host: "1.1.1.1" }
  - name: "Google DNS"
    host: "8.8.8.8"
    ping: { interval: "2s", dscp: 46 }        # per-target overrides
```

Per-target `ping`/`traceroute` blocks override `defaults` field-by-field.

---

## Web UI & API

- **Overview:** every target with status, RTT, loss, jitter, MOS and a 🎮
  *Gaming-ready* / ⚠ *Not ideal* verdict.
- **Target detail:** RTT chart with percentiles, plus an MTR-style traceroute table.
- **Local network:** the step-1 snapshot; a *Copy diagnostic report* button (localhost).

JSON API: `/api/meta`, `/api/targets`, `/api/targets/{name}/ping`,
`/api/targets/{name}/traceroute`, `/api/netinfo`, `/api/hoststat`, `/api/report`,
`/api/stream` (WebSocket live feed), and `/healthz`.

**Quit from the UI:** `POST /api/quit` gracefully stops the probe, but only when the
request comes from **loopback** (127.0.0.1 / ::1) — checked against the real TCP peer,
not forwarded headers. The button is hidden for non-localhost visitors.

---

## Metrics & security

The **Pushgateway is the primary export**; `/metrics` is always available too. They are
backed by two registries:

- **Base metrics** (ping/traceroute/quality/host) go to both `/metrics` and the push.
- **Environment metrics** (`proby_env_*`, containing local IP/MAC/route/ARP) are
  sensitive and go **only** via the authenticated pushgateway by default. They appear on
  local `/metrics` only if you set `web.metrics_include_environment: true`.

Every metric carries the `instance` label. Local `/metrics` never exposes local IP/MAC,
ARP/routing tables, or WiFi SSID/BSSID. Selected series:

```
proby_ping_rtt_seconds / _hist / _quantile_seconds{quantile}
proby_ping_loss_ratio, proby_ping_jitter_seconds, proby_ping_dscp
proby_quality_mos, proby_quality_gaming_ready, proby_target_up
proby_traceroute_hop_rtt_seconds{hop,hop_addr}, proby_traceroute_hop_loss_ratio
proby_iface_{rx,tx}_bytes_total, proby_iface_*_errors_total, proby_iface_speed_bps
proby_iface_link_up, proby_iface_link_changes_total, proby_iface_link_last_change_timestamp
proby_wifi_signal_dbm, proby_wifi_link_rate_bps         # Linux
proby_host_cpu_load_ratio, proby_host_mem_used_ratio
proby_push_last_success_timestamp, proby_push_failures_total
proby_env_info / proby_env_route / proby_env_arp        # push-only (sensitive)
```

---

## Persistence

If `history.enabled`, ping samples are written to an append-only ring file
(`history.path`, capped at `history.max_size`). On restart the recent history is
replayed so graphs survive restarts. Writes are batched and never block probing.

---

## Building

```bash
make build          # host binary
make build-all      # cross-compile the full matrix into dist/
make test           # unit tests
make vet            # go vet for host + linux + windows
make docker-linux   # Linux Docker image
```

Release artifacts (binary + `proby.example.yml`) are produced by
[goreleaser](https://goreleaser.com): `goreleaser release --clean`.

Binaries are static (`CGO_ENABLED=0`) with assets embedded, so each release is a single
self-contained file.

---

## Platform support

| Feature                | Linux | Windows | macOS |
|------------------------|:-----:|:-------:|:-----:|
| ICMP ping / traceroute |  ✅   |   ✅    |  ✅¹  |
| Network detection      |  ✅   |   ✅    |  ⏳   |
| Interface counters     |  ✅   |   ✅    |  ⏳   |
| WiFi signal            |  ✅   |   ⏳    |  ⏳   |
| CPU / memory           |  ✅   |   ✅    |  ⏳   |

¹ ICMP works on macOS via unprivileged datagram sockets. Network/host detection on
macOS is stubbed (soft-fails) and planned. Everything soft-fails gracefully, so proby
still runs on macOS today.
