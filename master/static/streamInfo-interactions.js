/* ================= GLOBAL SEARCH ================= */
function dropRender(matching) {
  const drop = $('searchDrop');
  if (!matching.length) { drop.hidden = true; return; }
  let html = '';
  if (matching.sources.length) {
    html += '<div class="drop-section"><div class="drop-label">IPs</div>' + matching.sources.slice(0, 8).map(s =>
      '<button class="drop-item" data-kind="ip" data-id="' + esc(s.ip) + '"><span class="kind" style="background:var(--cyan)"></span><span class="k">' + esc(s.ip) + (s.aliases && s.aliases.length ? ' · ' + esc(s.aliases[0]) : '') + '</span><span class="m">' + s.connections + ' sessions' + (s.active_now ? ' · active' : '') + '</span></button>'
    ).join('') + '</div>';
  }
  if (matching.destinations.length) {
    html += '<div class="drop-section"><div class="drop-label">Destinations</div>' + matching.destinations.slice(0, 8).map(x =>
      '<button class="drop-item" data-kind="destination" data-id="' + esc(x.endpoint.raw_address) + '"><span class="kind" style="background:var(--violet)"></span><span class="k">' + esc(endpointLabel(x.endpoint)) + '</span><span class="m">' + x.connections + ' sessions' + (x.active_now ? ' · active' : '') + '</span></button>'
    ).join('') + '</div>';
  }
  if (matching.routes.length) {
    html += '<div class="drop-section"><div class="drop-label">Routes</div>' + matching.routes.slice(0, 8).map(r =>
      '<button class="drop-item" data-kind="route" data-id="' + esc(r.id) + '"><span class="kind" style="background:var(--amber)"></span><span class="k">' + esc(endpointShort(r.listener)) + ':' + esc(r.listener.port || '') + ' → ' + esc(endpointLabel(r.destination)) + '</span><span class="m">' + r.connections + ' sessions</span></button>'
    ).join('') + '</div>';
  }
  drop.innerHTML = html;
  drop.hidden = false;
}

async function searchDropUpdate() {
  const q = $('searchInput').value.trim();
  const drop = $('searchDrop');
  if (q.length < 2) { drop.hidden = true; return; }
  try {
    const rp = new URLSearchParams({ ...rangeParams(), npm_only: state.npmOnly ? 'true' : 'false' }).toString();
    const res = await fetch(INTEL_ENDPOINT + '?q=' + encodeURIComponent(q) + (rp ? '&' + rp : '') + '&live=false&session_limit=1');
    if (!res.ok) return;
    const payload = await res.json();
    if (payload.search_matches) dropRender(payload.search_matches);
  } catch (e) { /* ignore */ }
}

/* ================= TABS & ROUTING ================= */
function switchTab(name) {
  state.tab = name;
  document.querySelectorAll('.tab-button').forEach(btn => {
    const on = btn.dataset.tab === name;
    btn.setAttribute('aria-selected', on ? 'true' : 'false');
  });
  document.querySelectorAll('.tab-panel').forEach(p => { p.hidden = p.id !== 'panel-' + name; });
  if (name === 'live') fetchLiveSockets().then(() => renderLiveTab(state.data || { live: {} }, Date.now()));
  if (name === 'overview' && state.data) renderOverviewChart((state.data.overview || {}).hourly || []);
  if (name === 'profile') renderProfileChart(((state.profile || {}).profile || {}).hourly || []);
}

function handleHash() {
  const hash = decodeURIComponent(location.hash || '');
  const parts = hash.replace(/^#\//, '').split('/').filter(Boolean);
  if (!parts.length) { switchTab('overview'); return; }
  const [kind, id] = parts;
  if (kind === 'ips' && id) { openProfile('ip', id); return; }
  if (kind === 'routes' && id) { openProfile('route', id); return; }
  if (kind === 'destinations' && id) { openProfile('destination', decodeURIComponent(id)); return; }
  if (kind === 'evidence') { switchTab('evidence'); return; }
  if (['overview', 'live', 'routes', 'ips', 'destinations', 'evidence'].includes(kind)) { switchTab(kind); return; }
  switchTab('overview');
}

/* ================= EVENTS ================= */
async function deleteEntityLogs(kind, value, label) {
  if (!confirm('Delete all historical logs for ' + label + '? This cannot be undone.')) return;
  try {
    const params = new URLSearchParams({ kind, value });
    const res = await fetch('/api/streamInfo/logs?' + params, { method: 'DELETE', headers: { 'Accept': 'application/json' } });
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw new Error(body.error || 'HTTP ' + res.status);
    }
    state.profile = null;
    state.profileTarget = null;
    await loadIntel();
  } catch (err) {
    const box = $('errorNotice');
    box.innerHTML = '<strong>Could not delete logs.</strong> ' + esc(err.message);
    box.hidden = false;
  }
}

function wireEvents() {
  // time range presets
  document.querySelectorAll('#rangePresets .rbtn').forEach(btn => {
    btn.addEventListener('click', () => applyRange(btn.dataset.range, '', ''));
  });
  $('rangeCustomApply').addEventListener('click', () => {
    const start = $('rangeStart').value;
    const end = $('rangeEnd').value;
    if (!start && !end) { applyRange('all', '', ''); return; }
    if (start && end && start >= end) {
      const box = $('errorNotice');
      box.innerHTML = '<strong>Invalid custom range.</strong> Start must be before end.';
      box.hidden = false;
      setTimeout(() => { if (box.innerHTML.includes('Invalid custom range')) box.hidden = true; }, 4000);
      return;
    }
    applyRange('custom', start, end);
  });

  // tabs
  document.querySelectorAll('.tab-button').forEach(btn => {
    btn.addEventListener('click', () => {
      history.replaceState(null, '', '#' + (btn.dataset.tab === 'overview' ? '/' : '#/' + btn.dataset.tab).replace('##', '#'));
      switchTab(btn.dataset.tab);
    });
  });

  // refresh
  $('refreshBtn').addEventListener('click', () => { loadIntel(); if (state.tab === 'live') fetchLiveSockets().then(() => renderLiveTab(state.data, Date.now())); });
  $('refreshBtn').addEventListener('click', () => { if (Date.now() - liveSocketsCache.at > 15000) liveSocketsCache.at = 0; });

  // global search
  let searchTimer = null;
  $('searchInput').addEventListener('input', () => {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(searchDropUpdate, 250);
  });
  $('searchInput').addEventListener('keydown', (e) => { if (e.key === 'Escape') $('searchDrop').hidden = true; });
  document.addEventListener('click', (e) => { if (!e.target.closest('.search-drop')) $('searchDrop').hidden = true; });
  $('searchForm').addEventListener('submit', (e) => {
    e.preventDefault();
    const q = $('searchInput').value.trim();
    $('searchDrop').hidden = true;
    if (!q) return;
    state.search.query = q;
    loadIntel({ q }).then(() => {
      if (state.data && state.data.search_matches) {
        const m = state.data.search_matches;
        if (m.sources.length === 1 && !m.destinations.length && !m.routes.length) { openProfile('ip', m.sources[0].ip); return; }
        if (m.destinations.length === 1 && !m.sources.length && !m.routes.length) { openProfile('destination', m.destinations[0].endpoint.raw_address); return; }
        if (m.routes.length === 1 && !m.sources.length && !m.destinations.length) { openProfile('route', m.routes[0].id); return; }
        switchTab('ips');
      }
    });
  });
  $('searchDrop').addEventListener('click', (e) => {
    const item = e.target.closest('.drop-item');
    if (!item) return;
    $('searchDrop').hidden = true;
    openProfile(item.dataset.kind, item.dataset.id);
  });

  // live filters
  const liveInputs = [['liveSearch', 'search', 'value'], ['liveScope', 'scope', 'value'], ['liveDirection', 'direction', 'value'], ['liveSort', 'sort', 'value'], ['liveHideLocal', 'hideLocal', 'checked']];
  liveInputs.forEach(([id, key, prop]) => {
    $(id).addEventListener('input', () => {
      state.filters.live[key] = $(id)[prop];
      renderLiveTab(state.data, Date.now());
    });
  });
  $('liveNpmOnly').addEventListener('change', () => {
    state.npmOnly = $('liveNpmOnly').checked;
    $('routeNpmOnly').checked = state.npmOnly;
    loadIntel();
  });

  // routes / ips / dests filters
   [['routes', 'routes', [['routeSearch', 'search', 'value'], ['routeState', 'state', 'value'], ['routeSort', 'sort', 'value']]],
   ['ips', 'ips', [['ipSearch', 'search', 'value'], ['ipState', 'state', 'value'], ['ipSort', 'sort', 'value']]],
   ['dests', 'dests', [['destSearch', 'search', 'value'], ['destState', 'state', 'value'], ['destSort', 'sort', 'value']]]].forEach(([_, key, defs]) => {
    defs.forEach(([id, fkey, prop]) => {
      $(id).addEventListener('input', () => {
        state.filters[key][fkey] = $(id)[prop];
        const d = state.data;
        if (!d) return;
        if (key === 'routes') renderRoutesTab(d, Date.now());
        else if (key === 'ips') renderIpsTab(d, Date.now());
        else renderDestsTab(d, Date.now());
     });
   });
  });
  $('routeNpmOnly').addEventListener('change', () => {
    state.npmOnly = $('routeNpmOnly').checked;
    $('liveNpmOnly').checked = state.npmOnly;
    loadIntel();
  });

  // evidence controls
  $('evEntityKind').addEventListener('change', () => {
    state.evidence.kind = $('evEntityKind').value;
    state.evidence.entity = '';
    renderEvidenceTab();
  });
  $('evEntity').addEventListener('change', () => { state.evidence.entity = $('evEntity').value; });
  $('evStart').addEventListener('change', () => { state.evidence.start = $('evStart').value; });
  $('evEnd').addEventListener('change', () => { state.evidence.end = $('evEnd').value; });
  $('evOutcome').addEventListener('change', () => { state.evidence.outcome = $('evOutcome').value; });
  $('evApply').addEventListener('click', runEvidenceReconstruct);
  $('evClear').addEventListener('click', () => {
    state.evidence = { kind: '', entity: '', start: '', end: '', outcome: '' };
    $('evEntityKind').value = ''; $('evEntity').value = ''; $('evStart').value = ''; $('evEnd').value = ''; $('evOutcome').value = '';
    state.evData = null;
    $('evTimeline').innerHTML = '<div class="empty"><h3>Pick a scope</h3><p>Choose an entity and time window, then press Reconstruct.</p></div>';
    $('evFacts').innerHTML = '';
    renderEvidenceTab();
  });

  // table row expansion + profile links (delegated)
  document.addEventListener('click', (e) => {
    const deleteButton = e.target.closest('[data-delete-kind]');
    if (deleteButton) {
      e.preventDefault();
      e.stopPropagation();
      deleteEntityLogs(deleteButton.dataset.deleteKind, deleteButton.dataset.deleteValue, deleteButton.dataset.deleteValue);
      return;
    }
    const link = e.target.closest('a[href^="#/"]');
    if (link) {
      e.preventDefault();
      e.stopPropagation();
      const href = decodeURIComponent(link.getAttribute('href'));
      const parts = href.replace(/^#\//, '').split('/');
      if (parts[0] === 'ips' && parts[1]) { openProfile('ip', parts.slice(1).join('/')); return; }
      if (parts[0] === 'routes' && parts[1]) { openProfile('route', parts.slice(1).join('/')); return; }
      if (parts[0] === 'destinations' && parts[1]) { openProfile('destination', decodeURIComponent(parts.slice(1).join('/'))); return; }
      if (parts[0] === 'evidence') { switchTab('evidence'); return; }
      if (parts[0]) switchTab(parts[0]);
      return;
    }
    const insight = e.target.closest('.insight, .metric.clickable');
    if (insight) {
      const linkAttr = insight.dataset.link || '';
      const goto = insight.dataset.goto;
      if (goto) { switchTab(goto); return; }
      if (linkAttr) {
        const parts = linkAttr.replace(/^#\//, '').split('/');
        if (parts[0] === 'ips' && parts[1]) { openProfile('ip', decodeURIComponent(parts[1])); return; }
        if (parts[0] === 'evidence') { switchTab('evidence'); return; }
      }
      return;
    }
    const ipBtn = e.target.closest('.ip-link');
    if (ipBtn) { e.stopPropagation(); openProfile('ip', ipBtn.dataset.ip); return; }
    const row = e.target.closest('tr[data-route], tr[data-ip], tr[data-dest], tr[data-liveid]');
    if (!row) return;
    if (row.dataset.route) {
      const id = row.dataset.route;
      state.expanded.routes.has(id) ? state.expanded.routes.delete(id) : state.expanded.routes.add(id);
      renderRoutesTab(state.data, Date.now());
    } else if (row.dataset.ip) {
      const id = row.dataset.ip;
      state.expanded.ips.has(id) ? state.expanded.ips.delete(id) : state.expanded.ips.add(id);
      renderIpsTab(state.data, Date.now());
    } else if (row.dataset.dest) {
      const id = row.dataset.dest;
      state.expanded.dests.has(id) ? state.expanded.dests.delete(id) : state.expanded.dests.add(id);
      renderDestsTab(state.data, Date.now());
    } else if (row.dataset.liveid) {
      const id = row.dataset.liveid;
      state.expanded.live.has(id) ? state.expanded.live.delete(id) : state.expanded.live.add(id);
      renderLiveTab(state.data, Date.now());
    }
  });

  // keyboard: Enter on rows/insights
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Enter') return;
    const t = e.target.closest?.('.insight, .metric.clickable, tr[data-route], tr[data-ip], tr[data-dest]');
    if (t) t.click();
  });

  window.addEventListener('hashchange', handleHash);
  window.addEventListener('resize', () => {
    if (state.data) renderOverviewChart((state.data.overview || {}).hourly || []);
    if (state.profile) renderProfileChart(((state.profile || {}).profile || {}).hourly || []);
  });
}

/* ================= BOOT ================= */
async function boot() {
  wireEvents();
  handleHash();
  await loadIntel();
  // prefetch live sockets so Live tab + overview are warm
  fetchLiveSockets().then(() => { if (state.data) renderLiveTab(state.data, Date.now()); });
  state.timer = setInterval(() => {
    if (document.hidden) return;
    loadIntel();
    fetchLiveSockets().then(() => { if (state.data) renderLiveTab(state.data, Date.now()); });
  }, REFRESH_MS);
}
boot();
