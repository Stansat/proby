# Grafana dashboards for proby

## `grafana-proby.json` — "Proby — Deployment overview"

A per-deployment operational view driven by the metrics proby pushes to the
Pushgateway (see the metric list in the repo `README.md`). Each probe tags its series
with its own `instance` (config `instance:` / `--instance`); on the Prometheus side that
identity is exposed as the **`site`** label (see *Label handling* below), which is the
deployment selector.

**Variables**

- `$site` (single-select) — the proby deployment. `label_values(proby_build_info, site)`.
- `$iface` (multi, includeAll) — filters the interface throughput / errors / link-state
  panels. `label_values(proby_iface_link_up{site=~"$site"}, iface)`. Narrow it when a probe
  has many virtual interfaces (the link-state timeline gets cramped otherwise).
- `$target` (multi, includeAll) — ICMP targets of that deployment. Drives the **repeated
  per-target row**. `label_values(proby_target_up{site=~"$site"}, target)`.

**Layout**

- *System / deployment* row: `proby_env_info` (hostname / egress IP+iface / local &
  gateway MAC / gateway IP / link type), routing table (`proby_env_route`), ARP table
  (`proby_env_arp`), CPU+memory, targets-up / last-push / push-failures stats, interface
  throughput (bps) and errors/drops, and a link-state (up/down) timeline.
- *Target: $target* (one row **per ICMP target**): RTT-distribution **heatmap**, RTT +
  p50/p95/p99 + jitter, packet-loss / availability, and an **MTR-style traceroute** table.

**Interface flaps** are marked across all time panels by a red Prometheus annotation:
`changes(proby_iface_link_last_change_timestamp{site=~"$site"}[$__interval]) > 0`.

### Label handling (`instance` → `site`)

Two pieces make multiple probes distinguishable:

1. **proby pusher** pushes with a per-instance grouping key
   (`push.New(url, job).Grouping("instance", instance)` in `internal/metrics/push.go`).
   Without it every probe pushes under grouping key `{job}` and clobbers the previous
   probe's group at the pushgateway — only the last writer would survive.
2. **Prometheus** scrapes the pushgateway *without* `honor_labels`, so the target's own
   `instance` (`pushgateway:9091`) wins and the pushed `instance` is renamed
   `exported_instance`. The `pushgateway` scrape job promotes it to `site` and drops the
   `exported_*` duplicates:

   ```yaml
   metric_relabel_configs:
     - source_labels: [exported_instance]
       regex: (.+)
       target_label: site
     - regex: exported_instance|exported_job
       action: labeldrop
   ```

   Net result on Prometheus: `site="<probe instance>"`, `instance="pushgateway:9091"`.

### Notes

- The RTT heatmap uses `proby_ping_rtt_seconds_hist_bucket` **raw** (`sum by (le) (...)`,
  no `rate()`): proby's histogram is a sliding-window snapshot, not a monotonic counter.
- The interface throughput / error panels set **Min step = `1m`** so `$__rate_interval`
  resolves to ≥ 4m. proby is scraped from the pushgateway every 60s and the Prometheus
  datasource has no `timeInterval` hint (defaults to 15s), so without this `rate()` over a
  ~60s window has < 2 samples and the panel shows "no data".
- The env / ARP / routing panels are populated only if the pushgateway export is enabled
  with `pushgateway.include_environment: true` (these series are push-only / sensitive).

### Deploy

The dashboard was imported into Grafana (`http://grafana4loki:3000`) in the
**InDevelopment** folder (uid `efn0o9wrw9og0a`) as uid `proby-deployment`. To re-import:

```sh
jq -n --argjson d "$(cat grafana-proby.json)" \
  '{dashboard:$d, folderUid:"efn0o9wrw9og0a", overwrite:true}' \
  | curl -s -X POST -H "Authorization: Bearer $GRAFANA_TOKEN" \
      -H "Content-Type: application/json" --data @- \
      http://grafana4loki:3000/api/dashboards/db
```
