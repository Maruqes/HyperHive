/* ================= OVERVIEW ================= */
function renderOverview(d, nowMs) {
  const ov = d.overview;
  if (!ov) {
    $('overviewMetrics').innerHTML = '';
    ['ovLivePanel', 'ovTopSources', 'ovTopDestinations', 'ovLongest', 'ovRecent', 'ovSpikes', 'ovInsightsPagination', 'ovLongestPagination', 'ovRecentPagination', 'ovSpikesPagination'].forEach(id => { $(id).innerHTML = ''; });
    $('ovTimelineCount').textContent = '';
    $('ovSpikeCount').textContent = '';
    $('ovInsightCount').textContent = '';
    $('ovInsights').innerHTML = '<div class="empty"><h3>Overview is unavailable in scoped search</h3><p>Clear the search scope to restore the full operational picture, or keep exploring the matching IPs, routes and destinations tabs.</p></div>';
    renderOverviewChart([]);
    return;
  }
  const w = ov.window || {};
  const metrics = [
    { label: 'Active conns', value: (d.live && d.live.established) || 0, cls: 'green', sub: (d.live ? d.live.total : 0) + ' sockets captured', click: 'live' },
    { label: 'Active IPs', value: (ov.active_ips || []).length, cls: 'green', sub: 'connected right now', click: 'ips' },
    { label: 'New IPs (24h)', value: w.new_ips ?? (ov.new_ips || []).length, cls: 'violet', sub: 'first seen today', click: 'ips' },
    { label: 'Sessions (24h)', value: w.connections ?? 0, cls: 'cyan', sub: (w.unique_sources ?? 0) + ' sources · ' + (w.unique_routes ?? 0) + ' routes' },
    { label: 'Failed (24h)', value: w.failed ?? 0, cls: (w.failed > 0 ? 'red' : ''), sub: w.connections ? pct(w.failed / w.connections) + ' of sessions' : '—' },
    { label: 'Routes active', value: (ov.active_routes || []).length, cls: 'green', sub: (d.routes || []).length + ' total observed', click: 'routes' }
  ];
  $('overviewMetrics').innerHTML = metrics.map(m =>
    '<div class="metric' + (m.click ? ' clickable' : '') + '"' + (m.click ? ' data-goto="' + m.click + '"' : '') + ' role="' + (m.click ? 'button' : 'group') + '" tabindex="0">' +
    '<div class="label">' + esc(m.label) + '</div><div class="value ' + (m.cls || '') + '">' + esc(m.value) + '</div><div class="sub">' + esc(m.sub) + '</div></div>'
  ).join('');

  // insights
  const insights = d.insights || [];
  const insightPage = pageItems(insights, 'insights');
  $('ovInsightCount').textContent = insights.length + ' findings';
  $('ovInsights').innerHTML = insights.length
    ? insightPage.items.map(i =>
      '<div class="insight" data-link="' + esc(i.link || '') + '" role="button" tabindex="0">' +
      '<span class="sev ' + esc(i.severity) + '"></span>' +
      '<div><div class="title">' + esc(i.title) + '</div><div class="detail">' + esc(i.detail) + '</div></div>' +
      '<span class="when">' + esc(ago(i.timestamp, nowMs)) + '</span></div>'
    ).join('')
    : '<div class="empty"><h3>Nothing unusual</h3><p>No anomalies, new IPs or long-lived sessions detected in this window.</p></div>';
  renderPagination('ovInsightsPagination', 'insights', insights.length, insightPage.totalPages);

  // timeline chart
  renderOverviewChart(ov.hourly || []);
  $('ovTimelineCount').textContent = (ov.hourly || []).length + ' buckets';
  const chartTitle = $('ovChartTitle');
  if (chartTitle) chartTitle.textContent = 'Connection timeline — ' + granularityLabel(d.query);

  // live panel
  const live = d.live || {};
  $('ovLivePanel').innerHTML =
    '<div class="kv">' +
    kv('Established', live.established ?? '—') + kv('Listening', live.listening ?? '—') +
    kv('Handshake', live.handshake ?? '—') + kv('Closing', live.closing ?? '—') +
    kv('Captured', live.captured_at ? ago(live.captured_at, nowMs) : '—') +
    '</div>' +
    ((ov.active_ips || []).length
       ? '<div class="chiprow" style="margin-top:8px">' + ov.active_ips.slice(0, 12).map(ip => '<span class="chip"><a href="#/ips/' + esc(ip) + '">' + esc(endpointLabel(sourceEndpoint(ip, d))) + '</a></span>').join('') + '</div>'
      : '<p class="faint" style="margin:6px 0 0">No external inbound connections in the live snapshot.</p>');

  // top talkers & destinations
  const bar = (max) => (count) => '<div class="minibar"><i style="width:' + Math.round(num(count) / max * 100) + '%"></i></div>';
  const topSrcMax = Math.max(1, ...(ov.top_sources || []).map(x => x.count));
  $('ovTopSources').innerHTML = (ov.top_sources || []).map(x =>
    '<div style="margin-bottom:6px"><div style="display:flex;justify-content:space-between;gap:8px"><a href="#/ips/' + esc(x.id) + '" class="mono" style="font-size:12px">' + esc(x.label) + '</a><span class="faint mono" style="font-size:11px">' + x.count + '</span></div>' + bar(topSrcMax)(x.count) + '</div>'
  ).join('') || '<p class="faint">No sources.</p>';
  const topDstMax = Math.max(1, ...(ov.top_destinations || []).map(x => x.count));
  $('ovTopDestinations').innerHTML = (ov.top_destinations || []).map(x =>
    '<div style="margin-bottom:6px"><div style="display:flex;justify-content:space-between;gap:8px"><a href="#/destinations/' + encodeURIComponent(x.id) + '" class="mono" style="font-size:12px">' + esc(x.label) + '</a><span class="faint mono" style="font-size:11px">' + x.count + '</span></div>' + bar(topDstMax)(x.count) + '</div>'
  ).join('') || '<p class="faint">No destinations.</p>';

  // longest sessions & recent
  const sessionRow = (s) =>
    '<div class="session"><div class="t">' + esc(fmtWhen(s.timestamp)) + '</div>' +
     '<div class="flow"><button class="ip-link" data-ip="' + esc(s.source.ip) + '">' + esc(endpointLabel(s.source)) + '</button>' +
    '<span class="arrow">→</span><span class="faint">' + esc(endpointShort(s.observed_listener)) + '</span>' +
    '<span class="arrow">→</span><a href="#/destinations/' + encodeURIComponent(s.destination.raw_address || ipPort(s.destination.ip, s.destination.port)) + '">' + esc(endpointShort(s.destination)) + '</a>' +
    (s.country && s.country !== 'Unknown' ? ' <span class="faint">· ' + esc(s.country) + '</span>' : '') + '</div>' +
    '<div class="meta">' + esc(fmtDur(s.session_seconds)) + ' · ' + fmtBytes(s.total_bytes) + '<br><span class="badge-out ' + esc(s.outcome) + '">' + esc(s.outcome) + '</span></div></div>';
  const longestPage = pageItems(ov.longest_sessions || [], 'longest');
  const recentPage = pageItems(ov.recent_sessions || [], 'recent');
  $('ovLongest').innerHTML = longestPage.items.map(sessionRow).join('') || '<div class="empty"><h3>No sessions</h3></div>';
  $('ovRecent').innerHTML = recentPage.items.map(sessionRow).join('') || '<div class="empty"><h3>No sessions</h3></div>';
  renderPagination('ovLongestPagination', 'longest', (ov.longest_sessions || []).length, longestPage.totalPages);
  renderPagination('ovRecentPagination', 'recent', (ov.recent_sessions || []).length, recentPage.totalPages);

  // spikes
  const spikes = ov.spikes || [];
  const spikePage = pageItems(spikes, 'spikes');
  $('ovSpikeCount').textContent = spikes.length ? spikes.length + ' detected' : 'none';
  $('ovSpikes').innerHTML = spikes.length
     ? spikePage.items.map(s =>
      '<div class="insight" data-link="#/evidence" role="button" tabindex="0">' +
      '<span class="sev ' + esc(s.severity) + '"></span>' +
      '<div><div class="title">' + (s.kind === 'failure_burst' ? 'Failure burst' : 'Connection spike') + ' at ' + esc(fmtWhen(s.timestamp)) + '</div>' +
      '<div class="detail">' + s.connections + ' connections' + (s.failed ? ' · ' + s.failed + ' failed (' + pct(s.failure_rate) + ')' : '') +
      (s.baseline ? ' · baseline ' + s.baseline.toFixed(1) + '/h' : '') + (s.z_score ? ' · z=' + s.z_score.toFixed(1) : '') + '</div></div>' +
      '<span class="when">' + esc(ago(s.timestamp, nowMs)) + '</span></div>'
    ).join('')
    : '<div class="empty"><h3>No spikes</h3><p>Hourly volume stayed within its rolling baseline.</p></div>';
  renderPagination('ovSpikesPagination', 'spikes', spikes.length, spikePage.totalPages);
}

function kv(k, v) { return '<div class="item"><div class="k">' + esc(k) + '</div><div class="v">' + esc(v) + '</div></div>'; }

/* ================= overview chart (no lib dependency) ================= */
let ovChart = null;
function renderOverviewChart(hourly) {
  const chart = $('ovChart');
  if (!chart) return;
  if (!hourly.length) {
    chart.innerHTML = '<div class="activity-empty">No activity in this window</div>';
    return;
  }
  const data = hourly.slice(-168);
  const width = 960, height = 250, pad = { l: 44, r: 18, t: 18, b: 34 };
  const plotW = width - pad.l - pad.r, plotH = height - pad.t - pad.b;
  const maxV = Math.max(1, ...data.map(p => num(p.connections)));
  const x = i => pad.l + (data.length === 1 ? plotW / 2 : i * plotW / (data.length - 1));
  const y = value => pad.t + plotH * (1 - num(value) / maxV);
  const points = data.map((p, i) => x(i) + ',' + y(p.connections)).join(' ');
  const failed = data.map((p, i) => x(i) + ',' + y(p.failed)).join(' ');
  const area = pad.l + ',' + (height - pad.b) + ' ' + points + ' ' + x(data.length - 1) + ',' + (height - pad.b);
  const grid = [0, 1, 2, 3, 4].map(step => {
    const value = Math.round(maxV * (1 - step / 4));
    const yy = pad.t + plotH * step / 4;
    return '<line x1="' + pad.l + '" x2="' + (width - pad.r) + '" y1="' + yy + '" y2="' + yy + '" stroke="#1e2d3a" stroke-width="1"/><text x="6" y="' + (yy + 4) + '" fill="#5d7488" font-family="IBM Plex Mono, monospace" font-size="10">' + value + '</text>';
  }).join('');
  const labels = data.map((p, i) => {
    const step = Math.max(1, Math.ceil(data.length / 7));
    if (i % step) return '';
    const d = new Date(p.timestamp);
    return '<text x="' + x(i) + '" y="' + (height - 10) + '" text-anchor="middle" fill="#5d7488" font-family="IBM Plex Mono, monospace" font-size="10">' + String(d.getDate()).padStart(2, '0') + '/' + String(d.getMonth() + 1).padStart(2, '0') + '</text>';
  }).join('');
  chart.innerHTML = '<svg style="display:block;width:100%;height:100%" viewBox="0 0 ' + width + ' ' + height + '" preserveAspectRatio="none" aria-hidden="true"><g>' + grid + '</g><polygon fill="rgba(56,217,232,.12)" points="' + area + '"></polygon><polyline fill="none" stroke="#38d9e8" stroke-width="2" vector-effect="non-scaling-stroke" points="' + points + '"></polyline><polyline fill="none" stroke="#f87171" stroke-width="2" vector-effect="non-scaling-stroke" stroke-dasharray="5 4" points="' + failed + '"></polyline><g>' + labels + '</g></svg>';
  ovChart = { data, maxV };
}
