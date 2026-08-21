
/* ================= LIVE NOW ================= */
function renderLiveTab(d, nowMs) {
  const live = d.live || {};
  const metrics = [
    { label: 'Established', value: live.established ?? 0, cls: (live.established ? 'green' : '') },
    { label: 'Listening', value: live.listening ?? 0, cls: 'cyan' },
    { label: 'Handshake', value: live.handshake ?? 0, cls: 'amber' },
    { label: 'Closing', value: live.closing ?? 0 },
    { label: 'Total sockets', value: live.total ?? 0 }
  ];
  $('liveMetrics').innerHTML = metrics.map(m =>
    '<div class="metric"><div class="label">' + esc(m.label) + '</div><div class="value ' + (m.cls || '') + '">' + esc(m.value) + '</div></div>'
  ).join('');
  $('liveCapMeta').textContent = live.captured_at ? 'captured ' + ago(live.captured_at, nowMs) : (live.available ? 'captured' : 'unavailable');

  const conns = collectLiveConnections(d);
  const f = state.filters.live;
  let rows = conns.filter(c => {
    if (f.hideLocal && c.local.ip && /^(127\.|::1$)/.test(c.local.ip) && (!c.remote.ip || /^(127\.|::1$)/.test(c.remote.ip))) return false;
    if (f.hideLocal && c.local.ip === '0.0.0.0' && c.remote.ip === '0.0.0.0') return false;
    if (f.scope === 'internal' && !(isPrivateIP(c.remote.ip) || isPrivateIP(c.local.ip))) return false;
    if (f.scope === 'external' && (isPrivateIP(c.remote.ip) || isPrivateIP(c.local.ip))) return false;
    if (f.direction === 'inbound' && c.direction !== 'inbound') return false;
    if (f.direction === 'outbound' && c.direction !== 'outbound') return false;
    if (f.npmOnly && !(c.streams || []).length) return false;
    if (f.search) {
      const hay = [c.local.ip, c.local.port, c.remote.ip, c.remote.port, c.interface_name, c.state, (c.streams || []).map(s => s.description || '').join(' ')].join(' ').toLowerCase();
      if (!hay.includes(f.search.toLowerCase())) return false;
    }
    return true;
  });
  rows.sort((a, b) => {
    if (f.sort === 'bytes') return (b.bytes_sent + b.bytes_received) - (a.bytes_sent + a.bytes_received);
    if (f.sort === 'retrans') return num(b.retrans) - num(a.retrans);
    if (f.sort === 'rtt') return num(b.rtt_ms) - num(a.rtt_ms);
    return 0; // capture order
  });
  const page = pageItems(rows, 'live');
  $('liveShown').textContent = rows.length + ' of ' + conns.length + ' sockets';
  $('liveEmpty').hidden = rows.length > 0;
  const body = $('liveTableBody');
  body.innerHTML = page.items.map(c => {
    const expanded = state.expanded.live.has(c.id);
    const dirLabel = c.direction === 'inbound' ? '⭢ inbound' : c.direction === 'outbound' ? '⭠ outbound' : '· local';
    const dirColor = c.direction === 'inbound' ? 'green' : c.direction === 'outbound' ? 'blue' : 'faint';
    return '<tr class="' + (expanded ? 'open' : '') + '" data-liveid="' + esc(c.id) + '">' +
      '<td><span class="state ' + (c.state === 'established' ? 'active' : c.state_group === 'listening' ? 'recent' : 'inactive') + '">' + esc(c.state) + '</span></td>' +
      '<td class="mono">' + esc(ipPort(c.local.ip, c.local.port)) + ' <span class="arrow">↔</span> ' + esc(ipPort(c.remote.ip, c.remote.port)) + '</td>' +
      '<td><span style="color:var(--' + dirColor + ')">' + dirLabel + '</span>' + (c.correlation_status && c.correlation_status !== 'unmatched' ? ' <span class="faint">· ' + esc(c.correlation_status) + '</span>' : '') + '</td>' +
      '<td class="wrap">' + ((c.streams || []).length ? c.streams.map(s => '<span class="chip">' + esc(s.description || ('stream #' + s.id)) + '</span>').join(' ') : '<span class="faint">—</span>') + '</td>' +
      '<td class="num">' + (c.rtt_ms != null ? c.rtt_ms.toFixed(1) + ' ms' : '—') + '</td>' +
      '<td class="num">' + fmtBytes(num(c.bytes_sent) + num(c.bytes_received)) + '</td>' +
      '<td class="num">' + (num(c.retrans) > 0 ? '<span style="color:var(--amber)">' + c.retrans + '</span>' : '0') + '</td>' +
      '<td class="mono dim">' + esc(c.interface_name || '—') + '</td>' +
      '<td class="mono dim">' + esc(c.uid) + '</td></tr>' +
      (expanded ? '<tr class="expand-row"><td colspan="9"><div class="expand-box">' + liveExpandBox(c) + '</div></td></tr>' : '');
  }).join('');
  renderPagination('livePagination', 'live', rows.length, page.totalPages);
}

function liveExpandBox(c) {
  const diags = [];
  if (c.rtt_ms != null) diags.push(['RTT', c.rtt_ms.toFixed(2) + ' ms']);
  if (c.rtt_min_ms != null) diags.push(['Min RTT', c.rtt_min_ms.toFixed(2) + ' ms']);
  if (c.rto_ms != null) diags.push(['RTO', c.rto_ms.toFixed(0) + ' ms']);
  if (c.bytes_sent != null) diags.push(['Sent', fmtBytes(c.bytes_sent)]);
  if (c.bytes_received != null) diags.push(['Received', fmtBytes(c.bytes_received)]);
  if (c.retrans != null) diags.push(['Retransmissions', c.retrans]);
  if (c.lost != null) diags.push(['Lost segments', c.lost]);
  if (c.unacked != null) diags.push(['Unacked', c.unacked]);
  if (c.cwnd != null) diags.push(['Congestion window', c.cwnd]);
  if (c.inode) diags.push(['Inode', c.inode]);
  return '<div class="kv">' + diags.map(([k, v]) => kv(k, v)).join('') +
    '<div class="item"><div class="k">Socket ID</div><div class="v">' + esc(c.id) + '</div></div></div>' +
    ((c.streams || []).length ? '<div class="chiprow">' + c.streams.map(s => '<span class="chip">stream #' + esc(s.id) + (s.enabled ? '' : ' · disabled') + ' · :' + esc(s.incoming_port) + ' → ' + esc(s.forwarding_host) + ':' + esc(s.forwarding_port) + '</span>').join('') + '</div>' : '');
}

// Live connections come from the intel payload's embedded snapshot. The intel endpoint
// does not return the raw socket list, so we fetch /active-connections when the tab opens.
let liveSocketsCache = { at: 0, data: [] };
async function fetchLiveSockets() {
  if (Date.now() - liveSocketsCache.at < 20000 && liveSocketsCache.data.length) return liveSocketsCache.data;
  try {
    const res = await fetch('/api/streamInfo/active-connections?state=all&limit=500');
    if (!res.ok) throw new Error('http ' + res.status);
    const payload = await res.json();
    liveSocketsCache = { at: Date.now(), data: (payload.connections || []).map(normalizeSocket) };
  } catch (e) { /* keep cache */ }
  return liveSocketsCache.data;
}
function normalizeSocket(c) {
  const ti = c.tcp_info || {};
  return {
    id: c.id, state: c.state, state_group: c.state_group,
    local: c.local || {}, remote: c.remote || {},
    interface_name: c.interface_name, uid: c.uid,
    direction: (c.correlation || {}).role === 'inbound_listener' ? 'inbound' : (c.correlation || {}).role === 'outbound_upstream' ? 'outbound' : 'local',
    correlation_status: (c.correlation || {}).status || '',
    streams: (c.correlation || {}).streams || [],
    rtt_ms: ti.rtt_ms != null ? ti.rtt_ms : null, rtt_min_ms: ti.min_rtt_ms != null ? ti.min_rtt_ms : null,
    rto_ms: ti.rto_ms != null ? ti.rto_ms : null,
    bytes_sent: ti.bytes_sent, bytes_received: ti.bytes_received,
    retrans: ti.total_retransmissions != null ? ti.total_retransmissions : ti.retransmissions,
    lost: ti.lost, unacked: ti.unacked, cwnd: ti.congestion_window, inode: c.inode
  };
}
function collectLiveConnections(d) { return liveSocketsCache.data; }
function isPrivateIP(ip) {
  if (!ip) return false;
  return /^(10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.|127\.|::1|fe80:|fd|0\.0\.0\.0)/.test(ip) || ip === '::1';
}

/* ================= ROUTES ================= */
function renderRoutesTab(d, nowMs) {
  const routes = d.routes || [];
  const active = routes.filter(r => r.active_now).length;
  $('routesMetrics').innerHTML = [
    { label: 'Routes observed', value: routes.length, cls: 'cyan' },
    { label: 'Active now', value: active, cls: active ? 'green' : '' },
    { label: 'Sessions through routes', value: routes.reduce((a, r) => a + r.connections, 0) },
    { label: 'Failed', value: routes.reduce((a, r) => a + r.failed, 0), cls: 'amber' }
  ].map(m => '<div class="metric"><div class="label">' + esc(m.label) + '</div><div class="value ' + (m.cls || '') + '">' + esc(m.value) + '</div></div>').join('');

  const f = state.filters.routes;
  const needle = f.search.toLowerCase();
  let rows = routes.filter(r => {
    if (f.state === 'active' && !r.active_now) return false;
    if (f.state === 'inactive' && r.active_now) return false;
    if (needle) {
      const hay = [r.listener.ip, r.listener.port, endpointShort(r.listener), r.destination.ip, r.destination.port, endpointShort(r.destination), r.protocol, (r.source_ips || []).join(' ')].join(' ').toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });
  rows.sort((a, b) => {
    if (f.sort === 'recent') return new Date(b.last_seen || 0) - new Date(a.last_seen || 0);
    if (f.sort === 'duration') return b.max_session_seconds - a.max_session_seconds;
    if (f.sort === 'traffic') return b.total_bytes - a.total_bytes;
    if (a.active_now !== b.active_now) return a.active_now ? -1 : 1;
    return b.connections - a.connections;
  });
  const page = pageItems(rows, 'routes');
  $('routesCount').textContent = rows.length + ' of ' + routes.length;
  $('routesEmpty').hidden = rows.length > 0;
  const maxSpark = Math.max(1, ...routes.map(r => Math.max(...(r.spark || [0]))));
  $('routesTableBody').innerHTML = page.items.map(r => {
    const expanded = state.expanded.routes.has(r.id);
    return '<tr class="' + (expanded ? 'open' : '') + '" data-route="' + esc(r.id) + '">' +
      '<td>' + stateBadge(r, nowMs) + (r.active_now && r.active_connections > 1 ? ' <span class="faint mono" style="font-size:10px">×' + r.active_connections + '</span>' : '') + '</td>' +
      '<td class="wrap"><div class="route-pipeline">' + routeSourcesHTML(r) + '<span class="pipeline-arrow">→</span><a class="pipeline-node listener" href="#/destinations/' + encodeURIComponent(r.listener.raw_address || ipPort(r.listener.ip, r.listener.port)) + '" onclick="event.stopPropagation()">' + esc(endpointShort(r.listener) + ':' + r.listener.port) + '</a><span class="pipeline-arrow">→</span><a class="pipeline-node destination" href="#/destinations/' + encodeURIComponent(r.destination.raw_address || ipPort(r.destination.ip, r.destination.port)) + '" onclick="event.stopPropagation()">' + esc(endpointLabel(r.destination)) + '</a></div><span class="chip">' + esc(r.protocol) + '</span></td>' +
      '<td class="mono dim">' + r.source_count + (r.source_count === 1 && r.source_ips[0] ? ' · ' + esc(r.source_ips[0]) : '') + '</td>' +
      '<td class="num">' + r.connections.toLocaleString() + '</td>' +
      '<td class="num">' + fmtBytes(r.total_bytes) + '</td>' +
      '<td class="num">' + (r.failed ? '<span style="color:var(--red)">' + r.failed + '</span>' : '0') + '</td>' +
      '<td class="num">' + fmtDur(r.avg_session_seconds) + ' / ' + fmtDur(r.max_session_seconds) + '</td>' +
      '<td class="mono dim" style="font-size:11px">' + esc(fmtWhen(r.first_seen)) + '<br>' + esc(fmtWhen(r.last_seen)) + '</td>' +
      '<td>' + sparkHTML(r.spark, maxSpark, r.spark_hours) + '</td></tr>' +
      (expanded ? '<tr class="expand-row"><td colspan="9"><div class="expand-box">' + routeExpandBox(r) + '</div></td></tr>' : '');
  }).join('');
  renderPagination('routesPagination', 'routes', rows.length, page.totalPages);
}

function routeSourcesHTML(r) {
  const sources = (r.source_ips || []).slice(0, 3);
  if (!sources.length) return '<span class="pipeline-node source">' + esc(r.source_count + ' sources') + '</span>';
  const links = sources.map(ip => '<a class="pipeline-node source" href="#/ips/' + esc(ip) + '" onclick="event.stopPropagation()">' + esc(endpointLabel(sourceEndpoint(ip))) + '</a>');
  if (r.source_count > sources.length) links.push('<span class="pipeline-node source">+' + (r.source_count - sources.length) + '</span>');
  return links.join('<span class="pipeline-more">, </span>');
}

function routeExpandBox(r) {
  const sources = (r.source_ips || []).slice(0, 30);
  return '<div class="kv">' +
    kv('Route ID', r.id) +
    kv('First seen', fmtWhen(r.first_seen, false)) +
    kv('Last seen', fmtWhen(r.last_seen, false)) +
    kv('Total sessions', r.connections.toLocaleString()) +
    kv('Avg / max session', fmtDur(r.avg_session_seconds) + ' / ' + fmtDur(r.max_session_seconds)) +
    kv('Traffic', fmtBytes(r.total_bytes)) +
    kv('Failed', r.failed + ' (' + pct(r.failure_rate) + ')') +
    kv('Stream match', r.stream_match_status) +
    '</div>' +
    '<div class="k faint" style="margin:8px 0 4px;font-family:var(--mono);font-size:10px;text-transform:uppercase">Sources using this route (' + r.source_count + ')</div>' +
    '<div class="chiprow">' + sources.map(ip => '<span class="chip"><a href="#/ips/' + esc(ip) + '">' + esc(ip) + '</a></span>').join('') + (r.source_count > sources.length ? '<span class="chip">+' + (r.source_count - sources.length) + ' more</span>' : '') + '</div>' +
     '<div class="entity-actions"><a href="#/routes/' + esc(r.id) + '">Open full route profile →</a><button class="danger-btn" type="button" data-delete-kind="route" data-delete-value="' + esc(r.id) + '">Delete route logs</button></div>';
}

/* ================= IPS ================= */
function renderIpsTab(d, nowMs) {
  const sources = d.sources || [];
  const active = sources.filter(s => s.active_now).length;
  const newOnes = sources.filter(s => s.new).length;
  $('ipsMetrics').innerHTML = [
    { label: 'IPs observed', value: sources.length, cls: 'cyan' },
    { label: 'Active now', value: active, cls: active ? 'green' : '' },
    { label: 'New (24h)', value: newOnes, cls: newOnes ? 'violet' : '' },
    { label: 'With signals', value: sources.filter(s => (s.signals || []).length).length, cls: 'amber' }
  ].map(m => '<div class="metric"><div class="label">' + esc(m.label) + '</div><div class="value ' + (m.cls || '') + '">' + esc(m.value) + '</div></div>').join('');

  const f = state.filters.ips;
  const needle = f.search.toLowerCase();
  let rows = sources.filter(s => {
    if (f.state === 'active' && !s.active_now) return false;
    if (f.state === 'recent' && !(s.recently_active || s.active_now)) return false;
    if (f.state === 'new' && !s.new) return false;
    if (f.state === 'signals' && !(s.signals || []).length) return false;
    if (needle) {
      const hay = [s.ip, (s.aliases || []).join(' '), (s.countries || []).join(' ')].join(' ').toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });
  rows.sort((a, b) => {
    if (f.sort === 'ip') return a.ip.localeCompare(b.ip, undefined, { numeric: true });
    if (f.sort === 'recent') return new Date(b.last_seen || 0) - new Date(a.last_seen || 0);
    if (f.sort === 'traffic') return b.total_bytes - a.total_bytes;
    if (f.sort === 'destinations') return b.unique_destinations - a.unique_destinations;
    if (a.active_now !== b.active_now) return a.active_now ? -1 : 1;
    return b.connections - a.connections;
  });
  const page = pageItems(rows, 'ips');
  $('ipsCount').textContent = rows.length + ' of ' + sources.length;
  $('ipsEmpty').hidden = rows.length > 0;
  const maxSpark = Math.max(1, ...sources.map(s => Math.max(...(s.spark || [0]))));
  $('ipsTableBody').innerHTML = page.items.map(s => {
    const expanded = state.expanded.ips.has(s.ip);
    return '<tr class="' + (expanded ? 'open' : '') + '" data-ip="' + esc(s.ip) + '">' +
      '<td>' + stateBadge(s, nowMs) + '</td>' +
      '<td class="wrap"><a class="mono" style="font-size:12px" href="#/ips/' + esc(s.ip) + '">' + esc(endpointLabel(s)) + '</a>' +
      (s.aliases && s.aliases.length ? '<br><span class="faint" style="font-size:10.5px">' + esc(s.aliases.join(', ')) + '</span>' : '') + '</td>' +
      '<td class="num">' + s.connections.toLocaleString() + '</td>' +
      '<td class="num">' + s.unique_destinations + '</td>' +
      '<td class="num">' + s.unique_routes + '</td>' +
      '<td class="num">' + fmtBytes(s.total_bytes) + '</td>' +
      '<td class="num">' + (s.failed ? '<span style="color:var(--red)">' + s.failed + '</span>' : '0') + '</td>' +
      '<td class="num">' + fmtDur(s.avg_session_seconds) + ' / ' + fmtDur(s.max_session_seconds) + '</td>' +
      '<td class="mono dim" style="font-size:11px">' + esc(fmtWhen(s.first_seen)) + '<br>' + esc(fmtWhen(s.last_seen)) + '</td>' +
      '<td>' + sparkHTML(s.spark, maxSpark, s.spark_hours) + '</td>' +
      '<td>' + signalsHTML(s.signals) + '</td></tr>' +
      (expanded ? '<tr class="expand-row"><td colspan="11"><div class="expand-box">' + ipExpandBox(s) + '</div></td></tr>' : '');
  }).join('');
  renderPagination('ipsPagination', 'ips', rows.length, page.totalPages);
}

function ipExpandBox(s) {
  return '<div class="kv">' +
    kv('Scope', s.scope) + kv('Countries', (s.countries || []).join(', ') || '—') +
    kv('First seen', fmtWhen(s.first_seen, false)) + kv('Last seen', fmtWhen(s.last_seen, false)) +
    kv('Sessions', s.connections.toLocaleString()) + kv('Failed', s.failed + ' (' + pct(s.failure_rate) + ')') +
    kv('Avg / max session', fmtDur(s.avg_session_seconds) + ' / ' + fmtDur(s.max_session_seconds)) +
    kv('Traffic', fmtBytes(s.total_bytes) + ' (↑ ' + fmtBytes(s.bytes_sent) + ' ↓ ' + fmtBytes(s.bytes_received) + ')') +
    kv('Ports', (s.ports || []).join(', ') || '—') + kv('Protocols', (s.protocols || []).join(', ') || '—') +
    '</div>' +
    '<div class="k faint" style="margin:8px 0 4px;font-family:var(--mono);font-size:10px;text-transform:uppercase">Destinations (' + s.unique_destinations + ')</div>' +
    '<div class="chiprow">' + (s.destinations || []).map(x => '<span class="chip"><a href="#/destinations/' + encodeURIComponent(x.id) + '">' + esc(x.label) + '</a> ' + x.count + '</span>').join('') + '</div>' +
    '<div class="entity-actions"><a href="#/ips/' + esc(s.ip) + '">Open full IP profile →</a><button class="danger-btn" type="button" data-delete-kind="source" data-delete-value="' + esc(s.ip) + '">Delete IP logs</button></div>';
}

/* ================= DESTINATIONS ================= */
function renderDestsTab(d, nowMs) {
  const dests = d.destinations || [];
  const active = dests.filter(x => x.active_now).length;
  $('destMetrics').innerHTML = [
    { label: 'Destinations', value: dests.length, cls: 'cyan' },
    { label: 'Active now', value: active, cls: active ? 'green' : '' },
    { label: 'Sessions', value: dests.reduce((a, x) => a + x.connections, 0) },
    { label: 'Unique source IPs', value: dests.reduce((a, x) => a + x.unique_sources, 0) }
  ].map(m => '<div class="metric"><div class="label">' + esc(m.label) + '</div><div class="value ' + (m.cls || '') + '">' + esc(m.value) + '</div></div>').join('');

  const f = state.filters.dests;
  const needle = f.search.toLowerCase();
  let rows = dests.filter(x => {
    if (f.state === 'active' && !x.active_now) return false;
    if (needle) {
      const hay = [x.endpoint.ip, x.endpoint.port, (x.endpoint.aliases || []).join(' '), (x.protocols || []).join(' ')].join(' ').toLowerCase();
      if (!hay.includes(needle)) return false;
    }
    return true;
  });
  rows.sort((a, b) => {
    if (f.sort === 'sources') return b.unique_sources - a.unique_sources;
    if (f.sort === 'recent') return new Date(b.last_seen || 0) - new Date(a.last_seen || 0);
    if (f.sort === 'traffic') return b.total_bytes - a.total_bytes;
    if (a.active_now !== b.active_now) return a.active_now ? -1 : 1;
    return b.connections - a.connections;
  });
  const page = pageItems(rows, 'dests');
  $('destCount').textContent = rows.length + ' of ' + dests.length;
  $('destEmpty').hidden = rows.length > 0;
  const maxSpark = Math.max(1, ...dests.map(x => Math.max(...(x.spark || [0]))));
  $('destTableBody').innerHTML = page.items.map(x => {
    const expanded = state.expanded.dests.has(x.endpoint.raw_address);
    return '<tr class="' + (expanded ? 'open' : '') + '" data-dest="' + esc(x.endpoint.raw_address) + '">' +
      '<td>' + stateBadge(x, nowMs) + '</td>' +
      '<td class="wrap"><a class="mono" style="font-size:12px" href="#/destinations/' + encodeURIComponent(x.endpoint.raw_address) + '">' + esc(endpointLabel(x.endpoint)) + '</a></td>' +
      '<td class="num">' + x.unique_sources + '</td>' +
      '<td class="num">' + x.connections.toLocaleString() + '</td>' +
      '<td class="num">' + fmtBytes(x.total_bytes) + '</td>' +
      '<td class="num">' + (x.failed ? '<span style="color:var(--red)">' + x.failed + '</span>' : '0') + '</td>' +
      '<td class="num">' + fmtDur(x.avg_session_seconds) + ' / ' + fmtDur(x.max_session_seconds) + '</td>' +
      '<td class="mono dim" style="font-size:11px">' + esc(fmtWhen(x.first_seen)) + '<br>' + esc(fmtWhen(x.last_seen)) + '</td>' +
      '<td>' + sparkHTML(x.spark, maxSpark, x.spark_hours) + '</td>' +
      '<td>' + signalsHTML(x.signals) + '</td></tr>' +
      (expanded ? '<tr class="expand-row"><td colspan="10"><div class="expand-box">' + destExpandBox(x) + '</div></td></tr>' : '');
  }).join('');
  renderPagination('destPagination', 'dests', rows.length, page.totalPages);
}

function destExpandBox(x) {
  return '<div class="kv">' +
    kv('IP', x.endpoint.ip || '—') + kv('Port', x.endpoint.port || '—') +
    kv('Scope', x.endpoint.scope || '—') + kv('Aliases', (x.endpoint.aliases || []).join(', ') || '—') +
    kv('First seen', fmtWhen(x.first_seen, false)) + kv('Last seen', fmtWhen(x.last_seen, false)) +
    kv('Sessions', x.connections.toLocaleString()) + kv('Failed', x.failed + ' (' + pct(x.failure_rate) + ')') +
    kv('Protocols', (x.protocols || []).join(', ') || '—') +
    '</div>' +
    '<div class="k faint" style="margin:8px 0 4px;font-family:var(--mono);font-size:10px;text-transform:uppercase">Top source IPs</div>' +
    '<div class="chiprow">' + (x.top_sources || []).map(s => '<span class="chip"><a href="#/ips/' + esc(s.id) + '">' + esc(s.label) + '</a> ' + s.count + '</span>').join('') + '</div>' +
    '<div class="entity-actions"><a href="#/destinations/' + encodeURIComponent(x.endpoint.raw_address) + '">Open full destination profile →</a><button class="danger-btn" type="button" data-delete-kind="destination" data-delete-value="' + esc(x.endpoint.raw_address) + '">Delete destination logs</button></div>';
}
