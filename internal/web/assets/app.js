"use strict";

// Minimal i18n dictionary; language chosen from server meta.
const I18N = {
  en: { quit: "Quit", targets: "Targets", target: "Target", ip: "IP", status: "Status",
    rtt: "RTT", loss: "Loss", jitter: "Jitter", verdict: "Verdict", latency: "Latency",
    traceroute: "Traceroute (MTR)", host: "Host", local_info: "Local network",
    copy_report: "Copy diagnostic report", gaming_ready: "Gaming ready", not_ideal: "Not ideal",
    down: "Down", confirm_quit: "Stop the proby probe on this machine?", copied: "Copied!",
    system: "System", throughput: "Throughput (egress)", memory: "Memory" },
  pl: { quit: "Zakończ", targets: "Cele", target: "Cel", ip: "IP", status: "Status",
    rtt: "RTT", loss: "Straty", jitter: "Jitter", verdict: "Werdykt", latency: "Opóźnienie",
    traceroute: "Traceroute (MTR)", host: "Host", local_info: "Sieć lokalna",
    copy_report: "Kopiuj raport diagnostyczny", gaming_ready: "Gotowe do gier", not_ideal: "Nie idealne",
    down: "Brak", confirm_quit: "Zatrzymać sondę proby na tej maszynie?", copied: "Skopiowano!",
    system: "System", throughput: "Przepływność (wyjście)", memory: "Pamięć" },
};
let L = I18N.en;
let meta = {};
let selected = null;
let latestTargets = [];
let egressIface = "";

function t(k) { return (L && L[k]) || k; }
function applyStaticI18n() {
  document.querySelectorAll("[data-i18n]").forEach(el => { el.textContent = t(el.dataset.i18n); });
}

// Duration helpers: Go time.Duration marshals as int64 nanoseconds.
function ms(ns) { return ns / 1e6; }
function fmtDur(ns) {
  if (!ns) return "—";
  const m = ms(ns);
  if (m < 1) return (m * 1000).toFixed(0) + " µs";
  return m.toFixed(m < 10 ? 2 : 1) + " ms";
}
function fmtPct(r) { return (r * 100).toFixed(1) + "%"; }

async function j(url) { const r = await fetch(url); if (!r.ok) throw new Error(r.status); return r.json(); }

async function init() {
  meta = await j("/api/meta");
  L = I18N[meta.lang] || I18N.en;
  applyStaticI18n();
  document.getElementById("instance").textContent = meta.instance || "";
  document.getElementById("version").textContent = meta.version || "";
  document.title = "proby — " + (meta.instance || "");

  if (meta.can_quit) {
    const qb = document.getElementById("quitBtn");
    qb.classList.remove("hidden");
    qb.addEventListener("click", doQuit);
  }
  if (meta.loopback) {
    const cr = document.getElementById("copyReport");
    cr.classList.remove("hidden");
    cr.addEventListener("click", copyReport);
  }
  document.getElementById("closeDetail").addEventListener("click", () => {
    selected = null; document.getElementById("detail").classList.add("hidden");
  });

  loadNetInfo();
  loadSystem();
  setInterval(loadSystem, 3000);
  if (meta.websocket && "WebSocket" in window) connectWS();
  else startPolling();
}

function setLive(on) {
  document.getElementById("conn").classList.toggle("live", on);
}

function connectWS() {
  const proto = location.protocol === "https:" ? "wss" : "ws";
  const ws = new WebSocket(`${proto}://${location.host}/api/stream`);
  ws.onopen = () => setLive(true);
  ws.onmessage = (ev) => { try { const m = JSON.parse(ev.data); if (m.targets) onTargets(m.targets); } catch (e) {} };
  ws.onclose = () => { setLive(false); setTimeout(connectWS, 2000); };
  ws.onerror = () => ws.close();
}

function startPolling() {
  const tick = async () => { try { onTargets(await j("/api/targets")); setLive(true); } catch (e) { setLive(false); } };
  tick(); setInterval(tick, 2000);
}

function onTargets(targets) {
  latestTargets = targets;
  renderOverview(targets);
  if (selected) {
    // Stats + hop table come live from the snapshot; the RTT chart is fetched.
    renderDetailStats(targets.find(x => x.name === selected));
    refreshChart();
  }
}

function verdictBadge(s) {
  if (!s.up) return `<span class="badge down">${t("down")}</span>`;
  if (s.gaming_ready) return `<span class="badge ready">🎮 ${t("gaming_ready")}</span>`;
  return `<span class="badge notready">⚠ ${t("not_ideal")}</span>`;
}

function renderOverview(targets) {
  const body = document.getElementById("targetsBody");
  body.innerHTML = "";
  for (const s of targets) {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td>${esc(s.name)}</td>
      <td class="mono">${esc(s.ip || "—")}</td>
      <td><span class="dot ${s.up ? "up" : "down"}"></span>${s.up ? "up" : "down"}</td>
      <td>${fmtDur(s.avg_rtt)}</td>
      <td>${fmtPct(s.loss_ratio)}</td>
      <td>${fmtDur(s.jitter)}</td>
      <td>${(s.mos || 0).toFixed(2)}</td>
      <td>${verdictBadge(s)}</td>`;
    tr.addEventListener("click", () => selectTarget(s.name));
    body.appendChild(tr);
  }
}

function selectTarget(name) {
  selected = name;
  document.getElementById("detail").classList.remove("hidden");
  document.getElementById("detailName").textContent = name;
  renderDetailStats(latestTargets.find(x => x.name === name));
  refreshChart();
}

// refreshChart fetches the (chart-only) ping time series for the selected target.
async function refreshChart() {
  if (!selected) return;
  try {
    drawChart(await j(`/api/targets/${encodeURIComponent(selected)}/ping`));
  } catch (e) {}
}

function renderDetailStats(s) {
  if (!s) return;
  document.getElementById("rttStats").innerHTML =
    `<span>last <b>${fmtDur(s.last_rtt)}</b></span>
     <span>avg <b>${fmtDur(s.avg_rtt)}</b></span>
     <span>p95 <b>${fmtDur(s.p95)}</b></span>
     <span>p99 <b>${fmtDur(s.p99)}</b></span>
     <span>min <b>${fmtDur(s.min_rtt)}</b></span>
     <span>max <b>${fmtDur(s.max_rtt)}</b></span>
     <span>loss <b>${fmtPct(s.loss_ratio)}</b></span>
     <span>jitter <b>${fmtDur(s.jitter)}</b></span>
     <span>MOS <b>${(s.mos||0).toFixed(2)}</b></span>`;
  // Hop table updates live from the snapshot (no extra fetch needed).
  renderHops(s.hops || []);
}

function drawChart(series) {
  const c = document.getElementById("rttChart");
  const ctx = c.getContext("2d");
  const W = c.width, H = c.height, pad = 6;
  ctx.clearRect(0, 0, W, H);
  const pts = series.filter(p => p.ok).map(p => ms(p.rtt));
  if (pts.length < 2) return;
  const max = Math.max(...pts) * 1.15 || 1;
  ctx.strokeStyle = "#58a6ff"; ctx.lineWidth = 1.5; ctx.beginPath();
  pts.forEach((v, i) => {
    const x = pad + (W - 2 * pad) * (i / (pts.length - 1));
    const y = H - pad - (H - 2 * pad) * (v / max);
    i ? ctx.lineTo(x, y) : ctx.moveTo(x, y);
  });
  ctx.stroke();
  // Draw loss markers as red ticks at bottom.
  ctx.fillStyle = "#f85149";
  series.forEach((p, i) => {
    if (!p.ok) {
      const x = pad + (W - 2 * pad) * (i / (series.length - 1));
      ctx.fillRect(x, H - pad - 4, 2, 4);
    }
  });
  ctx.fillStyle = "#8a97a6"; ctx.font = "11px sans-serif";
  ctx.fillText(max.toFixed(1) + " ms", 4, 12);
}

function renderHops(hops) {
  const body = document.getElementById("hopsBody");
  body.innerHTML = "";
  for (const h of hops) {
    const tr = document.createElement("tr");
    const host = h.host || h.addr || "*";
    const lossCls = h.loss_ratio > 0.1 ? "style='color:var(--red)'" : "";
    tr.innerHTML = `
      <td>${h.hop}</td>
      <td class="mono ${h.addr ? "" : "hopmuted"}">${esc(host)}</td>
      <td ${lossCls}>${fmtPct(h.loss_ratio)}</td>
      <td>${h.sent}</td>
      <td>${fmtDur(h.last_rtt)}</td>
      <td>${fmtDur(h.avg_rtt)}</td>
      <td>${fmtDur(h.best_rtt)}</td>
      <td>${fmtDur(h.worst_rtt)}</td>
      <td>${fmtDur(h.stddev_rtt)}</td>`;
    body.appendChild(tr);
  }
}

function fmtRate(bytesPerSec) {
  const bits = (bytesPerSec || 0) * 8;
  if (bits >= 1e6) return (bits / 1e6).toFixed(1) + " Mbps";
  if (bits >= 1e3) return (bits / 1e3).toFixed(0) + " Kbps";
  return bits.toFixed(0) + " bps";
}

function setBar(id, ratio) {
  const pct = Math.max(0, Math.min(100, (ratio || 0) * 100));
  const cls = pct >= 85 ? "crit" : pct >= 60 ? "warn" : "";
  document.getElementById(id).innerHTML =
    `<span class="bar-txt">${pct.toFixed(0)}%</span>` +
    `<div class="bar"><div class="bar-fill ${cls}" style="width:${pct}%"></div></div>`;
}

async function loadSystem() {
  try {
    const hs = await j("/api/hoststat");
    const tEl = document.getElementById("sysThroughput");
    let egr = null;
    if (egressIface && hs.interfaces) egr = hs.interfaces.find(x => x.name === egressIface);
    if (egr) {
      tEl.innerHTML = `<span class="down">↓ ${fmtRate(egr.rx_bps)}</span> &nbsp; ` +
        `<span class="up">↑ ${fmtRate(egr.tx_bps)}</span> ` +
        `<span class="hopmuted">(${esc(egr.name)})</span>`;
    } else {
      tEl.textContent = "—";
    }
    if (hs.cpu_load !== undefined) setBar("sysCpu", hs.cpu_load); else document.getElementById("sysCpu").textContent = "—";
    if (hs.mem_used !== undefined) setBar("sysMem", hs.mem_used); else document.getElementById("sysMem").textContent = "—";
  } catch (e) {}
}

async function loadNetInfo() {
  try {
    const ni = await j("/api/netinfo");
    egressIface = ni.egress_iface || "";
    const el = document.getElementById("netinfo");
    const rows = [];
    const add = (k, v) => { if (v !== undefined && v !== null && v !== "") rows.push(`<div><span class="k">${k}</span><span class="mono">${esc(String(v))}</span></div>`); };
    add("hostname", ni.hostname);
    add("link type", ni.link_type);
    add("egress interface", ni.egress_iface);
    if (meta.loopback) {
      add("egress IP", ni.egress_ip);
      add("default gateway", ni.default_gateway);
      add("local MAC", ni.local_mac);
      add("gateway MAC", ni.gateway_mac);
      add("routes", (ni.routing_table || []).length);
      add("arp entries", (ni.arp_table || []).length);
    } else {
      add("default route", ni.default_route_present ? "yes" : "no");
      add("interfaces", ni.interface_count);
    }
    el.innerHTML = rows.join("");
  } catch (e) {}
}

async function copyReport() {
  try {
    const r = await fetch("/api/report");
    const text = await r.text();
    await navigator.clipboard.writeText(text);
    const b = document.getElementById("copyReport");
    const old = b.textContent; b.textContent = t("copied");
    setTimeout(() => b.textContent = old, 1500);
  } catch (e) { alert("copy failed: " + e); }
}

async function doQuit() {
  if (!confirm(t("confirm_quit"))) return;
  try { await fetch("/api/quit", { method: "POST" }); } catch (e) {}
  document.body.innerHTML = "<main><h2>proby stopped.</h2></main>";
}

function esc(s) { return String(s).replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c])); }

init();
