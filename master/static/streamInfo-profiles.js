
/* ================= EVIDENCE ================= */
function renderEvidenceTab() {
  // populate entity selector from current data
  const d = state.data;
  if (!d) return;
  const kind = state.evidence.kind;
  const select = $('evEntity');
  const prev = state.evidence.entity;
  let options = [];
  if (kind === 'source') options = (d.sources || []).map(s => ({ id: s.ip, label: s.ip + (s.aliases && s.aliases.length ? ' · ' + s.aliases[0] : '') }));
  else if (kind === 'route') options = (d.routes || []).map(r => ({ id: r.id, label: endpointShort(r.listener) + ':' + r.listener.port + ' → ' + endpointLabel(r.destination) }));
  else if (kind === 'destination') options = (d.destinations || []).map(x => ({ id: x.endpoint.raw_address, label: endpointLabel(x.endpoint) }));
  select.disabled = !kind;
  select.innerHTML = '<option value="">Select entity…</option>' + options.map(o => '<option value="' + esc(o.id) + '"' + (o.id === prev ? ' selected' : '') + '>' + esc(o.label) + '</option>').join('');
}

async function runEvidenceReconstruct() {
  const ev = state.evidence;
  const params = {};
  if (ev.start || ev.end) {
    if (ev.start) params.start = localInputToRFC3339(ev.start);
    if (ev.end) params.end = localInputToRFC3339(ev.end);
  } else {
    Object.assign(params, rangeParams());
  }
  if (ev.kind === 'source' && ev.entity) params.source_ip = ev.entity;
  if (ev.kind === 'route' && ev.entity) params.route_id = ev.entity;
  if (ev.kind === 'destination' && ev.entity) params.destination_exact = ev.entity;
  if (ev.start) params.start = ev.start;
  if (ev.end) params.end = ev.end;
  if (ev.outcome) params.outcome = ev.outcome;
  params.live = 'false';
  params.npm_only = state.npmOnly ? 'true' : 'false';
  params.session_limit = '500';
  $('loadbar').hidden = false;
  try {
    const qs = buildQuery(params);
    const res = await fetch(INTEL_ENDPOINT + '?' + qs);
    if (!res.ok) throw new Error('HTTP ' + res.status);
    state.evData = await res.json();
    renderEvidenceResults();
  } catch (err) {
    $('evTimeline').innerHTML = '<div class="empty"><h3>Reconstruction failed</h3><p>' + esc(err.message) + '</p></div>';
  } finally {
    $('loadbar').hidden = true;
  }
}

function renderEvidenceResults() {
  const d = state.evData;
  if (!d) return;
  const sessions = d.sessions || [];
  $('evCount').textContent = sessions.length + ' sessions';
  const scopeBits = [];
  if (d.query && d.query.source_ip) scopeBits.push('IP ' + d.query.source_ip);
  if (d.query && d.query.route_id) scopeBits.push('route ' + d.query.route_id);
  if (d.query && d.query.destination_exact) scopeBits.push('destination ' + d.query.destination_exact);
  if (d.query && d.query.start) scopeBits.push('from ' + d.query.start);
  if (d.query && d.query.end) scopeBits.push('until ' + d.query.end);
  $('evTitle').textContent = scopeBits.length ? 'Session reconstruction — ' + scopeBits.join(' · ') : 'Session reconstruction';

  // narrative timeline, oldest → newest
  const ordered = [...sessions].reverse();
  const story = buildNarrative(ordered);
  $('evTimeline').innerHTML = story.length
    ? '<ol class="tl">' + story.map(step =>
      '<li class="tl-item ' + step.cls + '">' +
      '<div class="when">' + esc(step.when) + '</div>' +
      '<div class="what">' + step.what + '</div>' +
      (step.meta ? '<div class="meta">' + step.meta + '</div>' : '') +
      '</li>').join('') + '</ol>'
    : '<div class="empty"><h3>No sessions in scope</h3><p>Widen the time range or pick another entity.</p></div>';

  // facts summary
  const total = sessions.length;
  const failed = sessions.filter(s => s.outcome === 'failed').length;
  const bytes = sessions.reduce((a, s) => a + num(s.total_bytes), 0);
  const dur = sessions.reduce((a, s) => a + num(s.session_seconds), 0);
  const ips = new Set(sessions.map(s => s.source.ip));
  const dests = new Set(sessions.map(s => s.destination.raw_address || ipPort(s.destination.ip, s.destination.port)));
  const routes = new Set(sessions.map(s => s.route_id));
  $('evFacts').innerHTML = '<div class="kv">' +
    kv('Sessions', total.toLocaleString()) + kv('Failed', failed + (total ? ' (' + pct(failed / total) + ')' : '')) +
    kv('Time span', sessions.length ? fmtWhen(ordered[0].timestamp) + ' → ' + fmtWhen(sessions[0].timestamp) : '—') +
    kv('Total duration', fmtDur(dur)) + kv('Traffic', fmtBytes(bytes)) +
    kv('Distinct IPs', ips.size) + kv('Distinct destinations', dests.size) + kv('Distinct routes', routes.size) +
    '</div>' +
    '<div class="chiprow" style="margin-top:10px">' + [...ips].map(ip => '<span class="chip"><a href="#/ips/' + esc(ip) + '">' + esc(ip) + '</a></span>').join('') + '</div>';
}

function buildNarrative(orderedSessions) {
  const steps = [];
  let lastSource = null, lastDest = null;
  for (const s of orderedSessions) {
    const destLabel = s.destination.raw_address || ipPort(s.destination.ip, s.destination.port);
    const started = fmtWhen(s.timestamp, false);
    const ended = s.ended_at ? fmtWhen(s.ended_at, false) : '';
    if (s.source.ip !== lastSource) {
      steps.push({
        cls: s.outcome === 'failed' ? 'fail' : 'ok',
        when: started,
        what: '<strong>' + esc(s.source.ip) + '</strong>' + (s.country && s.country !== 'Unknown' ? ' <span class="faint">(' + esc(s.country) + ')</span>' : '') + ' connected',
        meta: null
      });
      lastSource = s.source.ip;
    }
    steps.push({
      cls: s.outcome === 'failed' ? 'fail' : 'ok',
      when: started,
      what: 'Used route <span class="route">' + esc(endpointShort(s.observed_listener)) + ':' + esc(s.observed_listener.port || '') + ' → ' + esc(endpointShort(s.destination)) + '</span>' +
        ' to reach <strong>' + esc(endpointLabel(s.destination)) + '</strong>',
      meta: esc(s.protocol) + ' · ' + fmtDur(s.session_seconds) + ' · ' + fmtBytes(s.total_bytes) + ' · ' + esc(s.outcome) +
        (ended ? ' · ended ' + esc(ended) : '') + ' · <a href="#/routes/' + esc(s.route_id) + '">route</a>'
    });
    if (s.outcome === 'failed') {
      steps.push({ cls: 'fail', when: started, what: 'Connection failed (status ' + esc(s.status) + ')', meta: null });
    }
    lastDest = destLabel;
  }
  if (steps.length) {
    const last = orderedSessions[orderedSessions.length - 1];
    if (last && last.ended_at) {
      steps.push({ cls: '', when: fmtWhen(last.ended_at, false), what: '<span class="faint">Last observed session ended (' + esc(fmtDur(last.session_seconds)) + ' duration)</span>', meta: null });
    }
  }
  return steps;
}

/* ================= PROFILE ================= */
async function openProfile(kind, id) {
  state.profileTarget = { kind, id };
  const params = { live: 'true', session_limit: '300', npm_only: state.npmOnly ? 'true' : 'false', ...rangeParams() };
  if (kind === 'ip') params.source_ip = id;
  else if (kind === 'route') params.route_id = id;
  else if (kind === 'destination') params.destination_exact = id;
  $('loadbar').hidden = false;
  try {
    const qs = buildQuery(params);
    const res = await fetch(INTEL_ENDPOINT + '?' + qs);
    if (!res.ok) throw new Error('HTTP ' + res.status);
    state.profile = await res.json();
    switchTab('profile');
    renderProfile(state.profile, Date.now());
    history.replaceState(null, '', '#/' + (kind === 'ip' ? 'ips/' : kind === 'route' ? 'routes/' : 'destinations/') + (kind === 'route' ? id : encodeURIComponent(id)));
  } catch (err) {
    const box = $('errorNotice');
    box.innerHTML = '<strong>Profile failed to load.</strong> ' + esc(err.message);
    box.hidden = false;
  } finally {
    $('loadbar').hidden = true;
  }
}

function renderProfile(d, nowMs) {
  if (!d || !d.profile) return;
  const p = d.profile;
  const head = $('profileHead');
  const facts = $('profileFacts');
  const winLabel = (d.query && d.query.range_label) || 'All time';
  const gran = granularityLabel(d.query);
  const winHTML = '<div class="faint" style="font-size:11px;margin-top:4px">Window: ' + esc(winLabel) + ' · ' + esc(gran) + ' buckets · <span id="profileRangeHint">change the time range above to re-scope this profile</span></div>';
  if (p.kind === 'source' && p.source) {
    const s = p.source;
    head.innerHTML =
      '<div><div class="big">' + esc(endpointLabel(s)) + '</div>' +
      '<div style="margin-top:8px">' + stateBadge(s, nowMs) + ' ' + signalsHTML(s.signals) + '</div>' + winHTML + '</div>' +
      '<div class="facts">' +
      '<div class="fact"><div class="k">Active conns</div><div class="v" style="color:var(--green)">' + s.active_connections + '</div></div>' +
      '<div class="fact"><div class="k">First seen</div><div class="v">' + esc(fmtWhen(s.first_seen)) + '</div></div>' +
      '<div class="fact"><div class="k">Last seen</div><div class="v">' + esc(fmtWhen(s.last_seen)) + '</div></div>' +
      '<div class="fact"><div class="k">Sessions</div><div class="v">' + s.connections.toLocaleString() + '</div></div>' +
      '</div>';
    facts.innerHTML = '<div class="kv">' +
      kv('Scope', s.scope) + kv('Countries', (s.countries || []).join(', ') || '—') +
      kv('Destinations', s.unique_destinations) + kv('Routes', s.unique_routes) +
      kv('Failed', s.failed + ' (' + pct(s.failure_rate) + ')') +
      kv('Avg / max session', fmtDur(s.avg_session_seconds) + ' / ' + fmtDur(s.max_session_seconds)) +
      kv('Traffic', fmtBytes(s.total_bytes) + ' (↑ ' + fmtBytes(s.bytes_sent) + ' ↓ ' + fmtBytes(s.bytes_received) + ')') +
      kv('Ports', (s.ports || []).join(', ') || '—') +
      '</div>' +
      '<div class="chiprow" style="margin-top:10px">' + (s.destinations || []).map(x => '<span class="chip"><a href="#/destinations/' + encodeURIComponent(x.id) + '">' + esc(x.label) + '</a> ' + x.count + '</span>').join('') + '</div>';
  } else if (p.kind === 'route' && p.route) {
    const r = p.route;
    head.innerHTML =
      '<div><div class="big mono" style="font-size:16px">' + esc(endpointLabel(r.listener)) + ' → ' + esc(endpointLabel(r.destination)) + '</div>' +
      '<div class="dim" style="margin-top:4px">' + esc(r.protocol) + ' · ' + esc(r.stream_match_status) + '</div>' +
      '<div style="margin-top:8px">' + stateBadge(r, nowMs) + ' ' + signalsHTML(r.signals) + '</div>' + winHTML + '</div>' +
      '<div class="facts">' +
      '<div class="fact"><div class="k">Active conns</div><div class="v" style="color:var(--green)">' + r.active_connections + '</div></div>' +
      '<div class="fact"><div class="k">Sources</div><div class="v">' + r.source_count + '</div></div>' +
      '<div class="fact"><div class="k">Sessions</div><div class="v">' + r.connections.toLocaleString() + '</div></div>' +
      '<div class="fact"><div class="k">Last seen</div><div class="v">' + esc(fmtWhen(r.last_seen)) + '</div></div>' +
      '</div>';
    facts.innerHTML = '<div class="kv">' +
      kv('Route ID', r.id) + kv('First seen', fmtWhen(r.first_seen, false)) + kv('Last seen', fmtWhen(r.last_seen, false)) +
      kv('Avg / max session', fmtDur(r.avg_session_seconds) + ' / ' + fmtDur(r.max_session_seconds)) +
      kv('Traffic', fmtBytes(r.total_bytes)) + kv('Failed', r.failed + ' (' + pct(r.failure_rate) + ')') +
      '</div>' +
      '<div class="chiprow" style="margin-top:10px">' + (r.source_ips || []).map(ip => '<span class="chip"><a href="#/ips/' + esc(ip) + '">' + esc(ip) + '</a></span>').join('') + '</div>' +
      ((r.streams || []).length ? '<div class="chiprow" style="margin-top:8px">' + r.streams.map(s => '<span class="chip">stream #' + esc(s.id) + (s.description ? ' · ' + esc(s.description) : '') + (s.enabled ? '' : ' · disabled') + '</span>').join('') + '</div>' : '');
  } else if (p.kind === 'destination' && p.destination) {
    const x = p.destination;
    head.innerHTML =
      '<div><div class="big mono" style="font-size:17px">' + esc(endpointLabel(x.endpoint)) + '</div>' +
      '<div class="dim" style="margin-top:4px">' + esc(x.endpoint.scope || '') + ' · ' + (x.protocols || []).join(', ') + '</div>' +
      '<div style="margin-top:8px">' + stateBadge(x, nowMs) + ' ' + signalsHTML(x.signals) + '</div>' + winHTML + '</div>' +
      '<div class="facts">' +
      '<div class="fact"><div class="k">Active conns</div><div class="v" style="color:var(--green)">' + x.active_connections + '</div></div>' +
      '<div class="fact"><div class="k">Unique sources</div><div class="v">' + x.unique_sources + '</div></div>' +
      '<div class="fact"><div class="k">Sessions</div><div class="v">' + x.connections.toLocaleString() + '</div></div>' +
      '<div class="fact"><div class="k">Last seen</div><div class="v">' + esc(fmtWhen(x.last_seen)) + '</div></div>' +
      '</div>';
    facts.innerHTML = '<div class="kv">' +
      kv('IP', x.endpoint.ip || '—') + kv('Port', x.endpoint.port || '—') +
      kv('Aliases', (x.endpoint.aliases || []).join(', ') || '—') +
      kv('First seen', fmtWhen(x.first_seen, false)) + kv('Last seen', fmtWhen(x.last_seen, false)) +
      kv('Avg / max session', fmtDur(x.avg_session_seconds) + ' / ' + fmtDur(x.max_session_seconds)) +
      kv('Traffic', fmtBytes(x.total_bytes)) + kv('Failed', x.failed + ' (' + pct(x.failure_rate) + ')') +
      kv('Countries', (x.countries || []).join(', ') || '—') +
      '</div>' +
      '<div class="chiprow" style="margin-top:10px">' + (x.top_sources || []).map(s => '<span class="chip"><a href="#/ips/' + esc(s.id) + '">' + esc(s.label) + '</a> ' + s.count + '</span>').join('') + '</div>';
  }
  renderProfileChart(p.hourly || []);

  // session timeline for this profile
  const sessions = (d.sessions || []).slice(0, 100);
  $('profileTimelineCount').textContent = sessions.length + ' sessions (newest first)';
  $('profileTimeline').innerHTML = sessions.map(s =>
    '<tr style="cursor:default"><td class="mono dim" style="width:130px">' + esc(fmtWhen(s.timestamp)) + '</td>' +
    '<td class="wrap"><span class="mono" style="font-size:12px">' +
    '<a href="#/ips/' + esc(s.source.ip) + '">' + esc(s.source.ip) + '</a> <span class="arrow">→</span> ' +
    esc(endpointShort(s.observed_listener)) + ':' + esc(s.observed_listener.port || '') + ' <span class="arrow">→</span> ' +
    '<a href="#/destinations/' + encodeURIComponent(s.destination.raw_address || ipPort(s.destination.ip, s.destination.port)) + '">' + esc(endpointShort(s.destination)) + '</a></span>' +
    '<div class="faint" style="font-size:10.5px">' + esc(s.protocol) + ' · ended ' + esc(s.ended_at ? fmtWhen(s.ended_at) : '—') + ' · <a href="#/routes/' + esc(s.route_id) + '">route</a></div></td>' +
    '<td class="num" style="width:90px">' + fmtDur(s.session_seconds) + '</td>' +
    '<td class="num" style="width:90px">' + fmtBytes(s.total_bytes) + '</td>' +
    '<td style="width:70px"><span class="badge-out ' + esc(s.outcome) + '">' + esc(s.outcome) + '</span></td></tr>'
  ).join('') || '<tr><td class="empty">No sessions in this window.</td></tr>';
}

function renderProfileChart(hourly) {
  const canvas = $('profileChart');
  if (!canvas) return;
  const box = canvas.parentElement.getBoundingClientRect();
  if (box.width < 50) return;
  const dpr = window.devicePixelRatio || 1;
  canvas.width = box.width * dpr;
  canvas.height = 240 * dpr;
  const ctx = canvas.getContext('2d');
  ctx.scale(dpr, dpr);
  const W = box.width, H = 240, pad = { l: 34, r: 8, t: 10, b: 20 };
  ctx.clearRect(0, 0, W, H);
  if (!hourly.length) { ctx.fillStyle = '#5d7488'; ctx.fillText('No activity in window', pad.l + 10, H / 2); return; }
  const data = hourly.slice(-72);
  const maxV = Math.max(1, ...data.map(p => p.connections));
  const bw = (W - pad.l - pad.r) / data.length;
  ctx.strokeStyle = '#1e2d3a'; ctx.fillStyle = '#5d7488'; ctx.font = '10px monospace';
  for (let g = 0; g <= 4; g++) {
    const y = pad.t + (H - pad.t - pad.b) * g / 4;
    ctx.beginPath(); ctx.moveTo(pad.l, y); ctx.lineTo(W - pad.r, y); ctx.stroke();
    ctx.fillText(String(Math.round(maxV * (1 - g / 4))), 4, y + 3);
  }
  data.forEach((p, i) => {
    const x = pad.l + i * bw;
    const h = (H - pad.t - pad.b) * p.connections / maxV;
    ctx.fillStyle = '#1a8a97';
    ctx.fillRect(x + 0.5, H - pad.b - h, Math.max(1, bw - 1), h);
    if (p.failed > 0) {
      ctx.fillStyle = '#f87171';
      const fh = (H - pad.t - pad.b) * p.failed / maxV;
      ctx.fillRect(x + 0.5, H - pad.b - h - Math.max(1, fh), Math.max(1, bw - 1), Math.max(1, fh));
    }
  });
  ctx.fillStyle = '#5d7488';
  const step = Math.ceil(data.length / 8);
  data.forEach((p, i) => {
    if (i % step === 0) {
      const dd = new Date(p.timestamp);
      ctx.fillText(String(dd.getDate()).padStart(2, '0') + '/' + String(dd.getMonth() + 1).padStart(2, '0'), pad.l + i * bw, H - 5);
    }
  });
}
