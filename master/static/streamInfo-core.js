'use strict';

const INTEL_ENDPOINT = '/api/streamInfo/intel';
const REFRESH_MS = 30000;

const state = {
  data: null, loading: false, timer: null, tab: 'overview',
  npmOnly: true,
  timeRange: { token: '7d', start: '', end: '' },
  profileTarget: null,
  expanded: { routes: new Set(), ips: new Set(), dests: new Set(), live: new Set() },
  filters: {
    live: { search: '', scope: '', direction: '', sort: 'recent', npmOnly: false, hideLocal: true },
    routes: { search: '', state: '', sort: 'activity', npmOnly: true },
    ips: { search: '', state: '', sort: 'activity' },
    dests: { search: '', state: '', sort: 'activity' }
  },
  evidence: { kind: '', entity: '', start: '', end: '', outcome: '' },
  search: { query: '', seq: 0, dropResults: null },
  profile: null, evData: null
};

/* ================= helpers ================= */
const $ = (id) => document.getElementById(id);
const esc = (value) => String(value ?? '').replace(/[&<>"']/g, (c) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const num = (v, d = 0) => { const n = Number(v); return Number.isFinite(n) ? n : d; };

function fmtBytes(value) {
  const b = Math.max(0, num(value));
  if (b < 1024) return Math.round(b) + ' B';
  const u = ['KB','MB','GB','TB','PB'];
  let cur = b, i = -1;
  do { cur /= 1024; i++; } while (cur >= 1024 && i < u.length - 1);
  return (cur >= 100 ? cur.toFixed(0) : cur >= 10 ? cur.toFixed(1) : cur.toFixed(2)) + ' ' + u[i];
}
function fmtDur(seconds) {
  const s = Math.max(0, num(seconds));
  if (s < 1) return Math.round(s * 1000) + 'ms';
  if (s < 60) return (s < 10 ? s.toFixed(1) : Math.round(s)) + 's';
  if (s < 3600) return Math.floor(s / 60) + 'm ' + Math.round(s % 60) + 's';
  return Math.floor(s / 3600) + 'h ' + Math.round((s % 3600) / 60) + 'm';
}
function fmtWhen(value, compact = true) {
  if (!value) return '—';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return String(value);
  const opts = compact
    ? { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }
    : { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' };
  return new Intl.DateTimeFormat(undefined, opts).format(d);
}
function ago(value, nowMs) {
  if (!value) return 'never';
  const t = new Date(value).getTime();
  if (!Number.isFinite(t)) return '—';
  const diff = Math.max(0, nowMs - t);
  if (diff < 60000) return Math.round(diff / 1000) + 's ago';
  if (diff < 3600000) return Math.round(diff / 60000) + 'm ago';
  if (diff < 86400000) return Math.round(diff / 3600000) + 'h ago';
  return Math.round(diff / 86400000) + 'd ago';
}
function pct(rate) { return (num(rate) * 100).toFixed(1) + '%'; }
function ipPort(ip, port) {
  if (!ip) return 'unknown';
  if (!port) return ip.includes(':') && !ip.startsWith('[') ? '[' + ip + ']' : ip;
  return (ip.includes(':') && !ip.startsWith('[') ? '[' + ip + ']' : ip) + ':' + port;
}
function endpointLabel(ep) {
  if (!ep || typeof ep !== 'object') return 'unknown';
  if (ep.aliases && ep.aliases.length) return ep.aliases[0] + ' (' + ipPort(ep.ip, ep.port) + ')';
  return ipPort(ep.ip, ep.port) || ep.raw_address || ep.display || 'unknown';
}
function endpointShort(ep) {
  if (!ep || typeof ep !== 'object') return 'unknown';
  if (ep.aliases && ep.aliases.length) return ep.aliases[0];
  return ep.ip || ep.raw_address || 'unknown';
}

/* ================= time range ================= */
function localInputToRFC3339(value) {
  if (!value) return '';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  return d.toISOString();
}

function rangeParams() {
  const tr = state.timeRange;
  if (tr.token === 'custom') {
    const p = {};
    if (tr.start) p.start = localInputToRFC3339(tr.start);
    if (tr.end) p.end = localInputToRFC3339(tr.end);
    return p;
  }
  return { range: tr.token };
}

function refreshRangeUI() {
  document.querySelectorAll('#rangePresets .rbtn').forEach(b => {
    b.classList.toggle('on', b.dataset.range === state.timeRange.token);
  });
  if (state.timeRange.token !== 'custom') {
    $('rangeStart').value = '';
    $('rangeEnd').value = '';
  }
}

function applyRange(token, start, end) {
  state.timeRange = { token, start: start || '', end: end || '' };
  refreshRangeUI();
  reloadWithRange();
}

function reloadWithRange() {
  if (state.tab === 'profile' && state.profileTarget) {
    openProfile(state.profileTarget.kind, state.profileTarget.id);
    return;
  }
  loadIntel();
}

/* ================= data loading ================= */
function buildQuery(extra = {}) {
  const params = new URLSearchParams();
  const map = { source_ip: 'source_ip', listener_ip: 'listener_ip', destination_ip: 'destination_ip', route_id: 'route_id', destination_exact: 'destination_exact', q: 'q', start: 'start', end: 'end', outcome: 'outcome', protocol: 'protocol', country: 'country', session_limit: 'session_limit', live: 'live', npm_only: 'npm_only' };
  for (const [k, v] of Object.entries(extra)) {
    if (v !== undefined && v !== null && v !== '') params.set(map[k] || k, v);
  }
  return params.toString();
}

async function loadIntel(extra = {}) {
  if (state.loading) return;
  state.loading = true;
  $('loadbar').hidden = false;
  $('refreshBtn').disabled = true;
  try {
    const query = { ...rangeParams(), npm_only: state.npmOnly ? 'true' : 'false', ...extra };
    const qs = buildQuery(query);
    const res = await fetch(INTEL_ENDPOINT + (qs ? '?' + qs : ''), { headers: { 'Accept': 'application/json' } });
    if (!res.ok) {
      let msg = 'HTTP ' + res.status;
      try { const body = await res.json(); if (body && body.error) msg = body.error; } catch (e) {}
      throw new Error(msg);
    }
    const payload = await res.json();
    state.data = payload;
    renderScopeBar(query.q || '');
    if (!query.q && !query.source_ip && !query.route_id && !query.destination_exact) {
      state.profile = null;
    }
    renderAll();
    $('errorNotice').hidden = true;
  } catch (err) {
    const box = $('errorNotice');
    box.innerHTML = '<strong>Failed to load analytics.</strong> ' + esc(err.message) + ' — showing last good data.';
    box.hidden = false;
  } finally {
    state.loading = false;
    $('loadbar').hidden = true;
    $('refreshBtn').disabled = false;
    $('initialSpinner').hidden = true;
    $('app').hidden = false;
  }
}

function renderScopeBar(q) {
  const bar = $('scopeBar');
  if (!q) { bar.hidden = true; bar.innerHTML = ''; return; }
  bar.hidden = false;
  bar.innerHTML = '<span class="fchip">Scoped to: ' + esc(q) +
    '<button id="scopeClear" title="Clear search scope" aria-label="Clear search scope">✕</button></span>' +
    '<span class="faint" style="font-size:11px;margin-left:8px">All tabs now show only matching evidence.</span>';
  $('scopeClear').addEventListener('click', () => {
    $('searchInput').value = '';
    loadIntel();
  });
}

/* ================= rendering root ================= */
function renderAll() {
  const d = state.data;
  if (!d) return;
  const nowMs = Date.now();
  renderWarnings(d);
  renderLivePill(d.live);
  renderRangeLabel(d);
  renderOverview(d, nowMs);
  renderLiveTab(d, nowMs);
  renderRoutesTab(d, nowMs);
  renderIpsTab(d, nowMs);
  renderDestsTab(d, nowMs);
  renderEvidenceTab();
  renderProfile(d, nowMs);
  updateTabCounts(d);
  const rangeLabel = (d.query && d.query.range_label) || 'All time';
  $('freshnessNote').textContent = rangeLabel + ' · generated ' + ago(d.generated_at, nowMs) + ' · auto-refresh 30s';
}

function renderRangeLabel(d) {
  const q = d.query || {};
  let label = q.range_label || 'All time';
  if (q.range === 'custom' && (q.window_start || q.window_end)) {
    label = (q.window_start ? fmtWhen(q.window_start) : '…') + ' → ' + (q.window_end ? fmtWhen(q.window_end) : 'now');
  }
  $('rangeLabel').textContent = label;
}

function granularityLabel(query) {
  try {
    const s = query && query.window_start ? new Date(query.window_start).getTime() : null;
    const e = query && query.window_end ? new Date(query.window_end).getTime() : null;
    if (s && e && e - s > 72 * 3600 * 1000) return 'daily';
    return 'hourly';
  } catch (err) { return 'hourly'; }
}

function renderWarnings(d) {
  const list = $('warnList');
  const warnings = (d.warnings || []).filter(w => w && !w.includes('may not describe historical state'));
  if (!warnings.length) { $('warnNotice').hidden = true; return; }
  list.innerHTML = warnings.map(w => '<li>' + esc(w) + '</li>').join('');
  $('warnNotice').hidden = false;
}

function renderLivePill(live) {
  const pill = $('livePill'), text = $('livePillText');
  if (live && live.available) {
    pill.classList.add('on');
    text.textContent = 'LIVE ' + (live.established || 0) + ' est.';
  } else {
    pill.classList.remove('on');
    text.textContent = 'LIVE OFF';
  }
}

function updateTabCounts(d) {
  const routes = d.routes || [], sources = d.sources || [], dests = d.destinations || [];
  $('tabRoutesCount').textContent = routes.length || '';
  $('tabIpsCount').textContent = sources.length || '';
  $('tabDestCount').textContent = dests.length || '';
  $('tabRoutesLive').hidden = !routes.some(r => r.active_now);
  $('tabIpsLive').hidden = !sources.some(s => s.active_now);
  $('tabDestLive').hidden = !dests.some(x => x.active_now);
}

/* ================= sparkline ================= */
function sparkHTML(spark, max, labels) {
  const values = spark || [];
  if (!values.length) return '<span class="faint">—</span>';
  const peak = Math.max(1, max || Math.max(...values));
  const keep = values.slice(-24);
  const daily = labels && labels.length && /^\d{2}-\d{2}$/.test(labels[0]);
  const title = (daily ? 'daily connections · last ' : 'hourly connections · last ') + keep.length + (daily ? ' days' : ' hours');
  return '<span class="spark" title="' + esc(title) + '">' +
    keep.map(v => {
      const h = Math.max(2, Math.round((num(v) / peak) * 22));
      return '<i' + (num(v) === peak && peak > 0 ? ' class="hot"' : '') + ' style="height:' + h + 'px"></i>';
    }).join('') + '</span>';
}

function stateBadge(entity, nowMs) {
  if (entity.active_now) return '<span class="state active">Active now</span>';
  if (entity.new) return '<span class="state new">New</span>';
  if (entity.last_seen && nowMs - new Date(entity.last_seen).getTime() < 3600000) return '<span class="state recent">Recent</span>';
  return '<span class="state inactive">Inactive</span>';
}

function signalsHTML(signals) {
  const labels = { new_ip: 'new ip', many_destinations: 'many dests', long_session: 'long session', failure_burst: 'failures' };
  return (signals || []).map(s => '<span class="sig ' + esc(s) + '">' + esc(labels[s] || s) + '</span>').join(' ');
}
