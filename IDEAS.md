# proby — Ideas & Future Features

A backlog of optional features, extra probes, and additional metrics beyond the core
PLAN.md scope. Nothing here is committed for v1; it's a menu for later iterations.
Each item notes rough value and cost so we can prioritise.

Legend — **Value**: ⭐ low … ⭐⭐⭐ high · **Cost**: 🔧 small … 🔧🔧🔧 large ·
Sensitive items flagged **[SEC]** (must respect the /metrics no-secrets rule) ·
✅ **PROMOTED** = pulled into core PLAN.md for v1.

---

## 1. Additional active probes

- **UDP & TCP traceroute** ⭐⭐ 🔧🔧 — many networks rate-limit/deny ICMP; TCP-SYN
  traceroute to a real port (e.g. 443) traverses firewalls better and is closer to
  what real traffic sees. (`traceroute.protocol: tcp|udp`.)
- **TCP connect check** ⭐⭐⭐ 🔧 — measure TCP handshake time to `host:port` per
  target (detects "ping OK but service down"). Metric `proby_tcp_connect_seconds`.
- **DNS resolution probe** ⭐⭐⭐ 🔧 — time A/AAAA lookups against configured resolvers;
  detect resolver failure/slowness (`proby_dns_lookup_seconds{resolver,qtype}`).
- **HTTP(S) probe** ⭐⭐⭐ 🔧🔧 — GET a URL per target: status code, TTFB, total time,
  TLS handshake time, cert expiry days. (blackbox-exporter-style.)
- **TLS certificate expiry** ⭐⭐ 🔧 — days-to-expiry + issuer per endpoint
  (`proby_tls_cert_expiry_seconds`).
- **Path MTU discovery** ⭐ 🔧🔧 — DF-bit probing to find the effective MTU to a target
  (catches VPN/tunnel MTU black-holes).
- **NTP offset / time sync check** ⭐⭐ 🔧🔧 — query NTP, report clock offset & stratum
  (bad clocks break TLS/auth).
- **Bufferbloat / load latency** ⭐⭐ 🔧🔧🔧 — RTT under load vs idle (basic
  responsiveness score; careful, generates traffic — off by default).
- **Speedtest / throughput** ⭐ 🔧🔧🔧 — opt-in bandwidth test to a chosen endpoint;
  heavy, off by default, scheduled infrequently.

## 2. Richer ICMP / latency analytics

- ✅ **PROMOTED — RTT histograms & percentiles** ⭐⭐⭐ 🔧 — native Prometheus histograms →
  server-side p50/p95/p99; better than gauges for alerting. *(now PLAN §8.1, §10)*
- **Per-hop MTR history graphs** ⭐⭐ 🔧🔧 — keep hop-RTT time-series, not just current
  aggregates; visualise route changes over time.
- **Route-change / path-flap detection** ⭐⭐⭐ 🔧🔧 — hash the hop path each cycle;
  emit an event + `proby_traceroute_path_changes_total` when the path changes.
- **Asymmetric-loss insight** ⭐ 🔧 — distinguish "hop doesn't reply to TTL-expired"
  (cosmetic) from real downstream loss (mtr already models this; surface it clearly).
- ✅ **PROMOTED — DSCP / ToS marking** ⭐ 🔧 — send probes with a configurable DSCP to
  test QoS paths. *(now PLAN §7.3)*
- ✅ **PROMOTED — Jitter & MOS estimate** ⭐⭐ 🔧 — VoIP-style Mean Opinion Score from
  latency/jitter/loss; surfaced in the UI as a 🎮 **"Gaming ready"** badge. *(now PLAN §8.2)*

## 3. Local machine / host metrics (mind [SEC])

- ✅ **PROMOTED — Interface counters** ⭐⭐ 🔧 — bytes/packets/errors/drops per egress
  iface (`proby_iface_*`), link speed, MTU. Non-sensitive numbers only. *(now PLAN §5.1)*
- ✅ **PROMOTED — WiFi signal quality** ⭐⭐⭐ 🔧🔧 — RSSI, SNR, link rate, channel, SSID
  **[SEC]** (SSID redacted from metrics). Huge for diagnosing flaky wireless customer
  sites. Linux `nl80211`/`iw`, Windows `WlanQueryInterface`. *(now PLAN §5.1)*
- **Gateway reachability & ARP stability** ⭐⭐ 🔧 — continuously ping the default
  gateway; separate "LAN healthy" from "WAN healthy".
- **DHCP lease info** ⭐ 🔧🔧 [SEC] — lease time remaining, DHCP server IP (gate/redact).
- ✅ **PROMOTED — Host resource pressure** ⭐ 🔧 — CPU/mem load (context for "probe looks
  slow"); minimal, optional. *(now PLAN §5.1)*
- **Captive-portal detection** ⭐⭐⭐ 🔧 — hit a known 204 endpoint; detect
  hotel/guest-wifi interception that breaks connectivity checks.
- **Public IP + geo/ASN** ⭐⭐ 🔧 [SEC] — query an external "what's my IP"/ASN service;
  detect ISP/CGNAT changes. Off by default (leaves the network, privacy).
- **IPv6 readiness** ⭐⭐ 🔧 — is IPv6 configured & does it actually reach the internet?

## 4. Alerting & events

- **Threshold alerts** ⭐⭐⭐ 🔧🔧 — per-target loss/latency thresholds → state changes
  surfaced in UI + optional webhook (Slack/Teams/generic POST).
- **Outage log / timeline** ⭐⭐⭐ 🔧🔧 — record up/down transitions with durations;
  "last 24h" incident timeline in the UI and via API.
- **Local notifications** ⭐ 🔧 — OS toast on outage (desktop deployments).
- **Syslog / journald export** ⭐ 🔧 — emit structured events for existing log pipelines.

## 5. Data & persistence

- ✅ **PROMOTED — On-disk history** ⭐⭐ 🔧🔧 — append-only ring file so history survives
  restarts; keeps single-binary. *(now PLAN §8.4)*
- **CSV/JSON export** ⭐⭐ 🔧 — download the sample window from the UI for offline
  analysis / sharing with the operator.
- **Prometheus remote_write** ⭐ 🔧🔧 — push directly to a TSDB without pushgateway.
- **OpenTelemetry export** ⭐ 🔧🔧 — OTLP metrics for orgs standardised on OTel.

## 6. Web UI enhancements

- ✅ **PROMOTED — Live WebSocket updates** ⭐⭐ 🔧🔧 — push instead of polling for smoother
  graphs; polling fallback retained. *(now PLAN §9)*
- **Dark mode & responsive/mobile** ⭐⭐ 🔧 — field engineers on phones.
- **Shareable snapshot** ⭐⭐⭐ 🔧🔧 — "export diagnostic bundle" (netinfo + recent
  samples + report) as a single JSON/HTML file to email the operator.
- **Comparative view** ⭐ 🔧 — overlay multiple targets' latency on one graph.
- **World map / hop geo** ⭐ 🔧🔧 [SEC] — plot traceroute hops on a map (needs geo
  lookups; external calls → opt-in).
- **PromQL-free built-in dashboards** ⭐⭐ 🔧🔧 — good defaults so customers don't need
  Grafana.

## 7. Operations & delivery

- **Config hot-reload** ⭐⭐ 🔧🔧 — watch `proby.yml`, apply target changes without
  restart (SIGHUP or fs-watch).
- **Self-update** ⭐ 🔧🔧🔧 — signed binary auto-update from a release URL. Security-
  sensitive; only with signature verification.
- **Service integration** ⭐⭐ 🔧 — ship systemd unit + Windows service wrapper
  (via `kardianos/service`) so it runs as a managed background service.
- **Remote control channel** ⭐ 🔧🔧🔧 [SEC] — outbound-only agent that lets ops trigger
  an on-demand traceroute/report from a central console. Powerful but needs auth+care.
- **Config profiles / templates** ⭐ 🔧 — ship ready-made `proby.yml` presets
  (e.g. "VoIP site", "branch office", "DNS focus").
- **Multi-probe fleet view** ⭐⭐⭐ 🔧🔧🔧 — optional central aggregator collecting from
  many deployed probes (this is a product-level step beyond a single binary).

## 8. Security & privacy hardening

- **Web UI auth** ⭐⭐⭐ 🔧🔧 — optional basic-auth/token + bind-to-localhost default so
  the UI isn't exposed on untrusted LANs.
- **TLS for UI/metrics** ⭐⭐ 🔧 — self-signed or provided cert for `/` and `/metrics`.
- **Metrics allow/deny lists** ⭐⭐ 🔧 — let the customer further restrict what `/metrics`
  exposes (belt-and-suspenders on the built-in [SEC] filter).
- **Redaction policy config** ⭐⭐ 🔧 — central switch controlling how aggressively
  IPs/MACs/SSIDs are hashed vs shown vs hidden across report/API/metrics.
- **Signed releases + SBOM** ⭐⭐ 🔧 — cosign/checksums + `govulncheck` in CI; matters
  when shipping a binary to customers.

---

## Suggested "next after v1" shortlist

If we pick a small high-value batch right after the core ships:

1. **TCP connect + DNS + HTTP probes** — turns proby from "is the pipe up" into
   "is the service usable". (⭐⭐⭐)
2. **RTT histograms + threshold alerts + outage timeline** — makes the data
   actionable, not just pretty. (⭐⭐⭐)
3. **WiFi signal metrics + captive-portal detection** — nails the two most common
   real-world "my internet is bad" root causes at customer sites. (⭐⭐⭐)
4. **Shareable diagnostic bundle** — directly serves the "send it to the operator"
   use case that motivates the whole tool. (⭐⭐⭐)
