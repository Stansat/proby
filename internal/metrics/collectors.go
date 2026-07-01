package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stansat/proby/internal/store"
)

// storeCollector emits ping/traceroute/quality metrics by reading the store on each
// scrape. Target hosts are exposed (they are the customer's own config); no local
// machine IP/MAC ever appears here.
type storeCollector struct {
	store *store.Store
}

var (
	descRTT      = prometheus.NewDesc("proby_ping_rtt_seconds", "Last ping RTT in seconds.", []string{"target", "host"}, nil)
	descRTTHist  = "proby_ping_rtt_seconds"
	descQuantile = prometheus.NewDesc("proby_ping_rtt_quantile_seconds", "Ping RTT quantiles over the window.", []string{"target", "host", "quantile"}, nil)
	descSent     = prometheus.NewDesc("proby_ping_sent_total", "Total ping probes sent.", []string{"target", "host"}, nil)
	descRecv     = prometheus.NewDesc("proby_ping_received_total", "Total ping replies received.", []string{"target", "host"}, nil)
	descLoss     = prometheus.NewDesc("proby_ping_loss_ratio", "Ping loss ratio (0-1) over the window.", []string{"target", "host"}, nil)
	descJitter   = prometheus.NewDesc("proby_ping_jitter_seconds", "Ping jitter (RFC 3550) in seconds.", []string{"target", "host"}, nil)
	descDSCP     = prometheus.NewDesc("proby_ping_dscp", "DSCP value used for probes.", []string{"target", "host"}, nil)
	descMOS      = prometheus.NewDesc("proby_quality_mos", "Estimated Mean Opinion Score (1.0-4.5).", []string{"target", "host"}, nil)
	descGaming   = prometheus.NewDesc("proby_quality_gaming_ready", "1 if the target meets the gaming-ready thresholds.", []string{"target", "host"}, nil)
	descUp       = prometheus.NewDesc("proby_target_up", "1 if the target is currently replying.", []string{"target", "host"}, nil)
	descHops     = prometheus.NewDesc("proby_traceroute_hops", "Number of traceroute hops discovered.", []string{"target", "host"}, nil)
	descHopRTT   = prometheus.NewDesc("proby_traceroute_hop_rtt_seconds", "Average RTT to a traceroute hop.", []string{"target", "host", "hop", "hop_addr"}, nil)
	descHopLoss  = prometheus.NewDesc("proby_traceroute_hop_loss_ratio", "Loss ratio at a traceroute hop.", []string{"target", "host", "hop", "hop_addr"}, nil)
)

func (c *storeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descRTT
	ch <- descQuantile
	ch <- descSent
	ch <- descRecv
	ch <- descLoss
	ch <- descJitter
	ch <- descDSCP
	ch <- descMOS
	ch <- descGaming
	ch <- descUp
	ch <- descHops
	ch <- descHopRTT
	ch <- descHopLoss
}

func (c *storeCollector) Collect(ch chan<- prometheus.Metric) {
	for _, s := range c.store.Snapshot() {
		l := []string{s.Name, s.Host}
		g := func(d *prometheus.Desc, v float64) {
			ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v, l...)
		}
		g(descRTT, s.LastRTT.Seconds())
		g(descLoss, s.LossRatio)
		g(descJitter, s.Jitter.Seconds())
		g(descDSCP, float64(s.DSCP))
		g(descMOS, s.MOS)
		g(descGaming, b2f(s.GamingReady))
		g(descUp, b2f(s.Up))
		g(descHops, float64(len(s.Hops)))
		ch <- prometheus.MustNewConstMetric(descSent, prometheus.CounterValue, float64(s.Sent), l...)
		ch <- prometheus.MustNewConstMetric(descRecv, prometheus.CounterValue, float64(s.Received), l...)

		ch <- prometheus.MustNewConstMetric(descQuantile, prometheus.GaugeValue, s.P50.Seconds(), s.Name, s.Host, "0.5")
		ch <- prometheus.MustNewConstMetric(descQuantile, prometheus.GaugeValue, s.P90.Seconds(), s.Name, s.Host, "0.9")
		ch <- prometheus.MustNewConstMetric(descQuantile, prometheus.GaugeValue, s.P95.Seconds(), s.Name, s.Host, "0.95")
		ch <- prometheus.MustNewConstMetric(descQuantile, prometheus.GaugeValue, s.P99.Seconds(), s.Name, s.Host, "0.99")

		if count, sum, buckets, ok := c.store.RTTHistogram(s.Name); ok && count > 0 {
			ch <- prometheus.MustNewConstHistogram(
				prometheus.NewDesc(descRTTHist+"_hist", "Histogram of ping RTTs.", []string{"target", "host"}, nil),
				count, sum, buckets, s.Name, s.Host)
		}

		for _, h := range s.Hops {
			hopLbl := strconv.Itoa(h.Hop)
			addr := h.Addr
			if addr == "" {
				addr = "*"
			}
			ch <- prometheus.MustNewConstMetric(descHopRTT, prometheus.GaugeValue, h.AvgRTT.Seconds(), s.Name, s.Host, hopLbl, addr)
			ch <- prometheus.MustNewConstMetric(descHopLoss, prometheus.GaugeValue, h.LossRatio, s.Name, s.Host, hopLbl, addr)
		}
	}
}

// envCollector emits the sensitive proby_env_* series from the latest snapshot.
type envCollector struct {
	provider EnvProvider
}

var (
	descEnvInfo   = prometheus.NewDesc("proby_env_info", "Local network environment (label-only info metric).", []string{"hostname", "egress_ip", "egress_iface", "link_type", "local_mac", "gateway_ip", "gateway_mac"}, nil)
	descEnvLink   = prometheus.NewDesc("proby_env_link_type", "Egress link type (1 for the active type).", []string{"link_type"}, nil)
	descEnvRoute  = prometheus.NewDesc("proby_env_route", "Routing table row (label-only info metric).", []string{"dst", "gateway", "iface", "metric"}, nil)
	descEnvARP    = prometheus.NewDesc("proby_env_arp", "ARP/neighbour entry (label-only info metric).", []string{"ip", "mac", "iface"}, nil)
	descEnvDefRt  = prometheus.NewDesc("proby_env_default_route_present", "1 if a default route was detected.", nil, nil)
	descEnvErrors = prometheus.NewDesc("proby_env_collect_errors", "1 per field that soft-failed during detection.", []string{"field"}, nil)
)

func (c *envCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descEnvInfo
	ch <- descEnvLink
	ch <- descEnvRoute
	ch <- descEnvARP
	ch <- descEnvDefRt
	ch <- descEnvErrors
}

func (c *envCollector) Collect(ch chan<- prometheus.Metric) {
	ni := c.provider()
	if ni == nil {
		return
	}
	ch <- prometheus.MustNewConstMetric(descEnvInfo, prometheus.GaugeValue, 1,
		ni.Hostname, ni.EgressIP, ni.EgressIface, string(ni.LinkType), ni.LocalMAC, ni.DefaultGateway, ni.GatewayMAC)
	ch <- prometheus.MustNewConstMetric(descEnvLink, prometheus.GaugeValue, 1, string(ni.LinkType))

	defPresent := 0.0
	for _, r := range ni.RoutingTable {
		if r.Destination == "default" {
			defPresent = 1
		}
		ch <- prometheus.MustNewConstMetric(descEnvRoute, prometheus.GaugeValue, 1,
			r.Destination, r.Gateway, r.Iface, strconv.Itoa(r.Metric))
	}
	ch <- prometheus.MustNewConstMetric(descEnvDefRt, prometheus.GaugeValue, defPresent)

	for _, e := range ni.ARPTable {
		ch <- prometheus.MustNewConstMetric(descEnvARP, prometheus.GaugeValue, 1, e.IP, e.MAC, e.Iface)
	}
	for field, st := range ni.Statuses {
		if !st.OK {
			ch <- prometheus.MustNewConstMetric(descEnvErrors, prometheus.GaugeValue, 1, field)
		}
	}
}

// hostCollector emits host-stats metrics. Only non-sensitive values are exported:
// interface names (not addresses), byte/error/drop counters, WiFi RSSI/rate/channel,
// and CPU/memory ratios. WiFi SSID/BSSID are deliberately NOT exported.
type hostCollector struct {
	provider HostProvider
}

var (
	descIfRx     = prometheus.NewDesc("proby_iface_rx_bytes_total", "Interface received bytes.", []string{"iface"}, nil)
	descIfTx     = prometheus.NewDesc("proby_iface_tx_bytes_total", "Interface transmitted bytes.", []string{"iface"}, nil)
	descIfRxErr  = prometheus.NewDesc("proby_iface_rx_errors_total", "Interface receive errors.", []string{"iface"}, nil)
	descIfTxErr  = prometheus.NewDesc("proby_iface_tx_errors_total", "Interface transmit errors.", []string{"iface"}, nil)
	descIfRxDrop = prometheus.NewDesc("proby_iface_rx_dropped_total", "Interface receive drops.", []string{"iface"}, nil)
	descIfTxDrop = prometheus.NewDesc("proby_iface_tx_dropped_total", "Interface transmit drops.", []string{"iface"}, nil)
	descIfSpeed  = prometheus.NewDesc("proby_iface_speed_bps", "Interface link speed (bits/s).", []string{"iface"}, nil)
	descIfLinkUp = prometheus.NewDesc("proby_iface_link_up", "Physical link state (1 = up/connected, 0 = down/disconnected).", []string{"iface"}, nil)
	descIfLinkCh = prometheus.NewDesc("proby_iface_link_changes_total", "Link up<->down transitions observed this session.", []string{"iface"}, nil)
	descIfLinkTs = prometheus.NewDesc("proby_iface_link_last_change_timestamp", "Unix time of the last link state change (0 if none observed).", []string{"iface"}, nil)
	descWifiSig  = prometheus.NewDesc("proby_wifi_signal_dbm", "WiFi RSSI in dBm.", []string{"iface"}, nil)
	descWifiRate = prometheus.NewDesc("proby_wifi_link_rate_bps", "WiFi link rate (bits/s).", []string{"iface"}, nil)
	descWifiChan = prometheus.NewDesc("proby_wifi_channel", "WiFi channel.", []string{"iface"}, nil)
	descHostCPU  = prometheus.NewDesc("proby_host_cpu_load_ratio", "Host CPU load (0-1).", nil, nil)
	descHostMem  = prometheus.NewDesc("proby_host_mem_used_ratio", "Host memory used (0-1).", nil, nil)
)

func (c *hostCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descIfRx
	ch <- descHostCPU
	ch <- descHostMem
}

func (c *hostCollector) Collect(ch chan<- prometheus.Metric) {
	hs := c.provider()
	if hs == nil {
		return
	}
	for _, i := range hs.Interfaces {
		l := []string{i.Name}
		ch <- prometheus.MustNewConstMetric(descIfRx, prometheus.CounterValue, float64(i.RxBytes), l...)
		ch <- prometheus.MustNewConstMetric(descIfTx, prometheus.CounterValue, float64(i.TxBytes), l...)
		ch <- prometheus.MustNewConstMetric(descIfRxErr, prometheus.CounterValue, float64(i.RxErrors), l...)
		ch <- prometheus.MustNewConstMetric(descIfTxErr, prometheus.CounterValue, float64(i.TxErrors), l...)
		ch <- prometheus.MustNewConstMetric(descIfRxDrop, prometheus.CounterValue, float64(i.RxDropped), l...)
		ch <- prometheus.MustNewConstMetric(descIfTxDrop, prometheus.CounterValue, float64(i.TxDropped), l...)
		ch <- prometheus.MustNewConstMetric(descIfSpeed, prometheus.GaugeValue, float64(i.SpeedBps), l...)
		ch <- prometheus.MustNewConstMetric(descIfLinkUp, prometheus.GaugeValue, b2f(i.LinkUp), l...)
		ch <- prometheus.MustNewConstMetric(descIfLinkCh, prometheus.CounterValue, float64(i.LinkChanges), l...)
		var lastChange float64
		if !i.LinkLastChange.IsZero() {
			lastChange = float64(i.LinkLastChange.Unix())
		}
		ch <- prometheus.MustNewConstMetric(descIfLinkTs, prometheus.GaugeValue, lastChange, l...)
	}
	if hs.WiFi != nil {
		l := []string{hs.WiFi.Iface}
		ch <- prometheus.MustNewConstMetric(descWifiSig, prometheus.GaugeValue, float64(hs.WiFi.SignalDBm), l...)
		ch <- prometheus.MustNewConstMetric(descWifiRate, prometheus.GaugeValue, float64(hs.WiFi.LinkRateBps), l...)
		ch <- prometheus.MustNewConstMetric(descWifiChan, prometheus.GaugeValue, float64(hs.WiFi.Channel), l...)
	}
	ch <- prometheus.MustNewConstMetric(descHostCPU, prometheus.GaugeValue, hs.CPULoad)
	ch <- prometheus.MustNewConstMetric(descHostMem, prometheus.GaugeValue, hs.MemUsed)
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
