package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"512SvMan/dnsmasq"
	"512SvMan/npm"

	"github.com/oschwald/geoip2-golang"
)

var regexpDailyLabel = regexp.MustCompile(`^\d{2}-\d{2}$`)

type intelHarness struct {
	dependencies intelDependencies
	entries      []streamLogEntry
	snapshot     *liveSnapshot
	now          time.Time
}

func newIntelHarness(entries []streamLogEntry, snapshot *liveSnapshot) *intelHarness {
	harness := &intelHarness{entries: entries, snapshot: snapshot, now: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)}
	harness.dependencies = intelDependencies{
		loadEntries: func() ([]streamLogEntry, *geoip2.Reader, error) {
			return harness.entries, nil, nil
		},
		getAliases: func() ([]dnsmasq.AliasEntry, error) {
			return []dnsmasq.AliasEntry{}, nil
		},
		listStreams: func(string, string) ([]npm.Stream, error) {
			return []npm.Stream{}, nil
		},
		getDescriptions: func(context.Context, string, []int) (map[int]string, error) {
			return map[int]string{}, nil
		},
		captureLive: func(context.Context, string) (*liveSnapshot, error) {
			if harness.snapshot == nil {
				return nil, context.DeadlineExceeded
			}
			return harness.snapshot, nil
		},
		now: func() time.Time { return harness.now },
	}
	return harness
}

func (harness *intelHarness) serve(query string) (*httptest.ResponseRecorder, intelResponse) {
	request := httptest.NewRequest(http.MethodGet, "/streamInfo/intel?"+query, nil)
	request = request.WithContext(context.WithValue(request.Context(), "token", "test-token"))
	recorder := httptest.NewRecorder()
	serveStreamIntel(recorder, request, harness.dependencies)
	var payload intelResponse
	if recorder.Code == http.StatusOK {
		_ = json.Unmarshal(recorder.Body.Bytes(), &payload)
	}
	return recorder, payload
}

func intelEntryAt(clientIP, listener, upstream string, at time.Time, seconds float64, status int) streamLogEntry {
	return streamLogEntry{
		ClientIP: clientIP, Country: "Portugal", Timestamp: at.UTC().Format(time.RFC3339),
		Protocol: "TCP", Status: status, BytesSent: 100, BytesReceived: 50,
		SessionTime: seconds, ProxyAddr: listener, UpstreamAddr: upstream, Time: at.UTC(),
	}
}

func TestIntelOverviewCountsLiveAndEntities(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-2*time.Hour), 60, 200),
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-1*time.Hour), 120, 200),
		intelEntryAt("10.0.0.9", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-30*time.Minute), 30, 500),
	}
	snapshot := &liveSnapshot{
		token: "tok", capturedAt: now.Add(-time.Minute), expiresAt: now.Add(time.Minute),
		connections: []liveConnection{
			{
				ID: "conn_a", State: "established", StateGroup: "active",
				Local:       analyticsEndpoint{IP: "192.168.1.175", Port: "25565"},
				Remote:      analyticsEndpoint{IP: "10.0.0.5", Port: "51000"},
				Correlation: liveCorrelation{Role: "inbound_listener", Status: "matched"},
			},
			{
				ID: "conn_b", State: "established", StateGroup: "active",
				Local:       analyticsEndpoint{IP: "192.168.1.175", Port: "52000"},
				Remote:      analyticsEndpoint{IP: "192.168.76.77", Port: "25565"},
				Correlation: liveCorrelation{Role: "outbound_upstream", Status: "matched"},
			},
			{
				ID: "conn_c", State: "listen", StateGroup: "listening",
				Local:  analyticsEndpoint{IP: "0.0.0.0", Port: "25565"},
				Remote: analyticsEndpoint{IP: "0.0.0.0", Port: ""},
			},
		},
		availability: liveAvailability{Available: true, TCP4: true},
	}
	harness := newIntelHarness(entries, snapshot)
	recorder, payload := harness.serve("live=true")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if payload.Mode != "overview" {
		t.Fatalf("expected overview mode, got %q", payload.Mode)
	}
	if payload.Live.Established != 2 || payload.Live.Listening != 1 {
		t.Fatalf("unexpected live summary: %+v", payload.Live)
	}
	if payload.Overview == nil {
		t.Fatal("expected overview payload")
	}
	if len(payload.Overview.ActiveIPs) != 1 || payload.Overview.ActiveIPs[0] != "10.0.0.5" {
		t.Fatalf("expected 10.0.0.5 as the only live active IP, got %v", payload.Overview.ActiveIPs)
	}
	if len(payload.Overview.ActiveRoutes) != 1 {
		t.Fatalf("expected one active route, got %v", payload.Overview.ActiveRoutes)
	}
	var activeSource *intelSource
	for i := range payload.Sources {
		if payload.Sources[i].IP == "10.0.0.5" {
			activeSource = &payload.Sources[i]
		}
	}
	if activeSource == nil || !activeSource.ActiveNow || activeSource.ActiveConnections != 1 {
		t.Fatalf("expected 10.0.0.5 active with one connection, got %+v", activeSource)
	}
	for _, route := range payload.Routes {
		wantActive := route.Destination.IP == "192.168.76.77"
		if route.ActiveNow != wantActive || (wantActive && route.ActiveConnections != 1) {
			t.Fatalf("route %s active=%v connections=%d", route.ID, route.ActiveNow, route.ActiveConnections)
		}
	}
	if len(payload.Sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(payload.Sessions))
	}
	if payload.Sessions[0].Timestamp < payload.Sessions[1].Timestamp {
		t.Fatal("expected newest sessions first")
	}
	if payload.Sessions[0].RouteID == "" {
		t.Fatal("expected route_id on sessions")
	}
	if payload.Overview.Window.Connections != 3 {
		t.Fatalf("expected 3 window connections, got %d", payload.Overview.Window.Connections)
	}
	if payload.Overview.Window.UniqueSources != 2 {
		t.Fatalf("expected 2 unique window sources, got %d", payload.Overview.Window.UniqueSources)
	}
}

func TestIntelSourceProfileAndSignals(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		intelEntryAt("10.0.0.7", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-2*time.Hour), 60, 200),
		intelEntryAt("10.0.0.7", "192.168.1.175:25566", "192.168.76.78:25566", now.Add(-90*time.Minute), 60, 200),
		intelEntryAt("10.0.0.7", "192.168.1.175:25567", "192.168.76.79:25567", now.Add(-80*time.Minute), 60, 500),
	}
	for i := 0; i < 9; i++ {
		entries = append(entries, intelEntryAt("10.0.0.7", "192.168.1.175:25567", "192.168.76.79:25567", now.Add(-70*time.Minute), 5, 500))
	}
	harness := newIntelHarness(entries, nil)
	recorder, payload := harness.serve("source_ip=10.0.0.7&live=false")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if payload.Mode != "source" {
		t.Fatalf("expected source mode, got %q", payload.Mode)
	}
	if payload.Profile == nil || payload.Profile.Source == nil {
		t.Fatal("expected source profile")
	}
	profile := payload.Profile.Source
	if profile.IP != "10.0.0.7" {
		t.Fatalf("unexpected profile IP %q", profile.IP)
	}
	if profile.Connections != 12 || profile.Failed != 10 {
		t.Fatalf("unexpected profile counters: %+v", profile)
	}
	if profile.UniqueDestinations != 3 || profile.UniqueRoutes != 3 || profile.UniqueListeners != 3 {
		t.Fatalf("unexpected uniqueness counts: %+v", profile)
	}
	if len(profile.Destinations) != 3 || len(profile.Routes) != 3 {
		t.Fatalf("expected related destinations and routes, got %d/%d", len(profile.Destinations), len(profile.Routes))
	}
	if len(payload.Sessions) != 12 {
		t.Fatalf("expected 12 sessions for the source, got %d", len(payload.Sessions))
	}
	if !containsString(profile.Signals, "new_ip") {
		t.Fatalf("expected new_ip signal, got %v", profile.Signals)
	}
	if !containsString(profile.Signals, "failure_burst") {
		t.Fatalf("expected failure_burst signal, got %v", profile.Signals)
	}
	if len(payload.Profile.Hourly) == 0 {
		t.Fatal("expected hourly activity for the profile")
	}
}

func TestIntelRouteProfile(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-2*time.Hour), 60, 200),
		intelEntryAt("10.0.0.9", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-1*time.Hour), 90, 200),
		intelEntryAt("10.0.0.5", "192.168.1.175:25566", "192.168.76.88:25566", now.Add(-30*time.Minute), 10, 200),
	}
	harness := newIntelHarness(entries, nil)
	routeID := intelRouteID(intelRouteKey("192.168.1.175:25565", "192.168.76.77:25565", "TCP"))
	recorder, payload := harness.serve("route_id=" + routeID + "&live=false")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if payload.Profile == nil || payload.Profile.Route == nil {
		t.Fatal("expected route profile")
	}
	route := payload.Profile.Route
	if route.Connections != 2 || route.SourceCount != 2 {
		t.Fatalf("unexpected route counters: %+v", route)
	}
	if len(route.SourceIPs) != 2 {
		t.Fatalf("expected both source IPs, got %v", route.SourceIPs)
	}
	if len(payload.Sessions) != 2 {
		t.Fatalf("expected 2 sessions on the route, got %d", len(payload.Sessions))
	}
	for _, session := range payload.Sessions {
		if session.RouteID != routeID {
			t.Fatalf("session route mismatch: %s != %s", session.RouteID, routeID)
		}
	}
}

func TestIntelRouteAndDestinationProfilesNotFound(t *testing.T) {
	entries := []streamLogEntry{
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC), 60, 200),
	}
	harness := newIntelHarness(entries, nil)
	recorder, _ := harness.serve("route_id=route_000000000000&live=false")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown route, got %d", recorder.Code)
	}
	recorder, _ = harness.serve("destination_exact=10.9.9.9:80&live=false")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown destination, got %d", recorder.Code)
	}
}

func TestIntelDestinationProfile(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-2*time.Hour), 60, 200),
		intelEntryAt("10.0.0.9", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-1*time.Hour), 90, 200),
	}
	harness := newIntelHarness(entries, nil)
	recorder, payload := harness.serve("destination_exact=192.168.76.77:25565&live=false")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if payload.Profile == nil || payload.Profile.Destination == nil {
		t.Fatal("expected destination profile")
	}
	destination := payload.Profile.Destination
	if destination.UniqueSources != 2 || len(destination.TopSources) != 2 {
		t.Fatalf("unexpected destination sources: %+v", destination)
	}
	if len(payload.Sessions) != 2 {
		t.Fatalf("expected 2 sessions for the destination, got %d", len(payload.Sessions))
	}
}

func TestIntelSearchMatchesEntities(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		intelEntryAt("192.168.1.43", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-2*time.Hour), 60, 200),
		intelEntryAt("10.0.0.5", "192.168.1.175:25566", "192.168.1.43:25566", now.Add(-1*time.Hour), 60, 200),
	}
	harness := newIntelHarness(entries, nil)
	recorder, payload := harness.serve("q=192.168.1.43&live=false")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if payload.Mode != "search" {
		t.Fatalf("expected search mode, got %q", payload.Mode)
	}
	if payload.SearchMatches == nil {
		t.Fatal("expected search matches")
	}
	if payload.SearchMatches.Interpreted != "ip" {
		t.Fatalf("expected IP interpretation, got %q", payload.SearchMatches.Interpreted)
	}
	if len(payload.SearchMatches.Sources) != 1 || payload.SearchMatches.Sources[0].IP != "192.168.1.43" {
		t.Fatalf("expected source match for 192.168.1.43, got %+v", payload.SearchMatches.Sources)
	}
	if len(payload.SearchMatches.Destinations) != 1 {
		t.Fatalf("expected destination match for 192.168.1.43, got %+v", payload.SearchMatches.Destinations)
	}
	if payload.FilteredEntries != 2 {
		t.Fatalf("expected both entries to match the IP search, got %d", payload.FilteredEntries)
	}
}

func TestIntelSearchSubstringFiltersEntries(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-2*time.Hour), 60, 200),
		intelEntryAt("10.0.0.9", "192.168.1.175:25566", "192.168.76.55:25566", now.Add(-1*time.Hour), 60, 200),
	}
	harness := newIntelHarness(entries, nil)
	_, payload := harness.serve("q=25566&live=false")
	if payload.FilteredEntries != 1 {
		t.Fatalf("expected one entry matching port 25566, got %d", payload.FilteredEntries)
	}
	if len(payload.Sessions) != 1 || payload.Sessions[0].ObservedListener.Port != "25566" {
		t.Fatalf("unexpected session after search: %+v", payload.Sessions)
	}
}

func TestIntelInvalidParamsRejected(t *testing.T) {
	harness := newIntelHarness(nil, nil)
	recorder, _ := harness.serve("route_id=notaroute&live=false")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid route id, got %d", recorder.Code)
	}
	recorder, _ = harness.serve("session_limit=0&live=false")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid session limit, got %d", recorder.Code)
	}
	recorder, _ = harness.serve("start=notadate&live=false")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid start, got %d", recorder.Code)
	}
}

func TestIntelLiveUnavailableDegradesGracefully(t *testing.T) {
	entries := []streamLogEntry{
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC), 60, 200),
	}
	harness := newIntelHarness(entries, nil)
	recorder, payload := harness.serve("live=true")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 despite live failure, got %d", recorder.Code)
	}
	if payload.Live.Available {
		t.Fatal("live should be unavailable")
	}
	found := false
	for _, warning := range payload.Warnings {
		if strings.Contains(warning, "live socket snapshot is unavailable") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected live degradation warning, got %v", payload.Warnings)
	}
	for _, source := range payload.Sources {
		if source.ActiveNow {
			t.Fatal("no source should be active without live data")
		}
	}
}

func TestIntelSpikesAndInsights(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	entries := make([]streamLogEntry, 0)
	for hour := 0; hour < 12; hour++ {
		at := now.Add(time.Duration(12-hour) * -time.Hour)
		entries = append(entries, intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", at, 60, 200))
	}
	for i := 0; i < 30; i++ {
		entries = append(entries, intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-30*time.Minute), 60, 200))
	}
	harness := newIntelHarness(entries, nil)
	_, payload := harness.serve("live=false")
	if payload.Overview == nil {
		t.Fatal("expected overview")
	}
	if len(payload.Overview.Spikes) == 0 {
		t.Fatal("expected at least one spike")
	}
	if len(payload.Insights) == 0 {
		t.Fatal("expected insights")
	}
	for _, insight := range payload.Insights {
		switch insight.Severity {
		case "critical", "warning", "info":
		default:
			t.Fatalf("unexpected severity %q", insight.Severity)
		}
		if insight.Link == "" || insight.Title == "" {
			t.Fatalf("insight missing link or title: %+v", insight)
		}
	}
}

func TestIntelLongSessionInsight(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-6*time.Hour), 5*3600+300, 200),
	}
	harness := newIntelHarness(entries, nil)
	_, payload := harness.serve("live=false")
	found := false
	for _, insight := range payload.Insights {
		if insight.Category == "long_session" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected long_session insight, got %v", payload.Insights)
	}
}

func TestIntelSessionEndedAtAndNewestFirst(t *testing.T) {
	base := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", base, 120, 200),
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", base.Add(time.Hour), 60, 200),
	}
	harness := newIntelHarness(entries, nil)
	_, payload := harness.serve("live=false")
	if len(payload.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(payload.Sessions))
	}
	if payload.Sessions[0].Timestamp <= payload.Sessions[1].Timestamp {
		t.Fatal("expected newest first")
	}
	ended, err := time.Parse(time.RFC3339, payload.Sessions[0].EndedAt)
	if err != nil {
		t.Fatalf("ended_at is not RFC3339: %q", payload.Sessions[0].EndedAt)
	}
	expected := base.Add(time.Hour).Add(60 * time.Second)
	if !ended.Equal(expected) {
		t.Fatalf("expected ended_at %s, got %s", expected, ended)
	}
}

func TestIntelRouteIDValidation(t *testing.T) {
	if !intelValidRouteID("route_0123456789ab") {
		t.Fatal("valid route id rejected")
	}
	for _, invalid := range []string{"", "route_0123456789a", "route_0123456789ABC", "path_0123456789ab", "route_0123456789ag"} {
		if intelValidRouteID(invalid) {
			t.Fatalf("invalid route id accepted: %q", invalid)
		}
	}
}

func TestIntelDegradesWhenLogMissing(t *testing.T) {
	harness := newIntelHarness(nil, nil)
	harness.dependencies.loadEntries = func() ([]streamLogEntry, *geoip2.Reader, error) {
		return nil, nil, os.ErrNotExist
	}
	recorder, _ := harness.serve("live=false")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when log missing, got %d", recorder.Code)
	}
}

func TestIntelServesEmptyCollections(t *testing.T) {
	harness := newIntelHarness([]streamLogEntry{}, nil)
	recorder, payload := harness.serve("live=false")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty log, got %d", recorder.Code)
	}
	if payload.Sources == nil || payload.Routes == nil || payload.Destinations == nil || payload.Sessions == nil {
		t.Fatal("expected non-nil empty collections")
	}
}

func TestIntelAliasesEnrichEndpoints(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-time.Hour), 60, 200),
	}
	harness := newIntelHarness(entries, nil)
	harness.dependencies.getAliases = func() ([]dnsmasq.AliasEntry, error) {
		return []dnsmasq.AliasEntry{{IP: "192.168.76.77", Alias: "minecraft.internal"}}, nil
	}
	_, payload := harness.serve("live=false")
	if len(payload.Destinations) != 1 {
		t.Fatalf("expected one destination, got %d", len(payload.Destinations))
	}
	if !containsString(payload.Destinations[0].Endpoint.Aliases, "minecraft.internal") {
		t.Fatalf("expected alias enrichment, got %+v", payload.Destinations[0].Endpoint)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestIntelRangeTokenFiltersWindow(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-10*24*time.Hour), 60, 200),
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-2*24*time.Hour), 60, 200),
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-1*24*time.Hour), 60, 200),
	}
	harness := newIntelHarness(entries, nil)
	recorder, payload := harness.serve("range=7d&live=false")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if payload.FilteredEntries != 2 {
		t.Fatalf("expected 2 entries within 7d, got %d", payload.FilteredEntries)
	}
	if payload.Query.Range != "7d" || payload.Query.RangeLabel != "Last 7 days" {
		t.Fatalf("unexpected range echo: %+v", payload.Query)
	}
	if payload.Query.WindowStart == "" || payload.Query.WindowEnd == "" {
		t.Fatalf("expected window bounds, got %+v", payload.Query)
	}
	if payload.Overview == nil || payload.Overview.TotalConnections != 2 {
		t.Fatalf("expected overview scoped to window: %+v", payload.Overview)
	}
}

func TestIntelRangeTokenAliasesAndInvalid(t *testing.T) {
	harness := newIntelHarness(nil, nil)
	for _, token := range []string{"24h", "1d", "7d", "14d", "1mo", "30d", "3mo", "90d", "6mo", "180d", "1y", "365d", "all"} {
		recorder, payload := harness.serve("range=" + token + "&live=false")
		if recorder.Code != http.StatusOK || payload.Query.Range != token {
			t.Fatalf("token %s rejected: %d", token, recorder.Code)
		}
	}
	recorder, _ := harness.serve("range=5x&live=false")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid range token, got %d", recorder.Code)
	}
}

func TestIntelCustomTimestampWindow(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-10*24*time.Hour), 60, 200),
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-5*24*time.Hour), 60, 200),
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-24*time.Hour), 60, 200),
	}
	harness := newIntelHarness(entries, nil)
	start := now.Add(-6 * 24 * time.Hour).Format(time.RFC3339)
	end := now.Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	recorder, payload := harness.serve("start=" + start + "&end=" + end + "&live=false")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if payload.FilteredEntries != 1 {
		t.Fatalf("expected 1 entry in custom window, got %d", payload.FilteredEntries)
	}
	if payload.Query.Range != "custom" || payload.Query.RangeLabel != "Custom range" {
		t.Fatalf("unexpected custom range echo: %+v", payload.Query)
	}
}

func TestIntelCustomDateOnlyWindow(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-48*time.Hour), 60, 200),
		intelEntryAt("10.0.0.9", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-24*time.Hour), 60, 200),
	}
	harness := newIntelHarness(entries, nil)
	recorder, payload := harness.serve("start=2026-08-19&end=2026-08-20&live=false")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	// end date is inclusive of the whole day: covers Aug 19 and Aug 20
	if payload.FilteredEntries != 2 {
		t.Fatalf("expected 2 entries with inclusive end date, got %d", payload.FilteredEntries)
	}
	if len(payload.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(payload.Sessions))
	}
	recorder, _ = harness.serve("start=notadate&live=false")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad start, got %d", recorder.Code)
	}
	recorder, _ = harness.serve("start=2026-08-21&end=2026-08-19&live=false")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for inverted range, got %d", recorder.Code)
	}
}

func TestIntelRangeAppliesToProfiles(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-40*24*time.Hour), 60, 200),
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-2*24*time.Hour), 90, 200),
	}
	harness := newIntelHarness(entries, nil)
	recorder, payload := harness.serve("source_ip=10.0.0.5&range=7d&live=false")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if payload.Profile == nil || payload.Profile.Source == nil {
		t.Fatal("expected source profile")
	}
	if payload.Profile.Source.Connections != 1 {
		t.Fatalf("expected 1 connection within 7d profile, got %d", payload.Profile.Source.Connections)
	}
	if payload.Query.Range != "7d" {
		t.Fatalf("expected range echo on profile, got %q", payload.Query.Range)
	}
}

func TestIntelAdaptiveDailySeries(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	entries := make([]streamLogEntry, 0)
	for day := 0; day < 30; day++ {
		at := now.Add(-time.Hour).Add(time.Duration(day-29) * 24 * time.Hour)
		entries = append(entries, intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", at, 60, 200))
	}
	harness := newIntelHarness(entries, nil)
	_, payload := harness.serve("range=30d&live=false")
	if payload.Overview == nil {
		t.Fatal("expected overview")
	}
	series := payload.Overview.Hourly
	if len(series) < 28 || len(series) > 32 {
		t.Fatalf("expected ~30 daily buckets, got %d", len(series))
	}
	for _, point := range series {
		if !strings.HasSuffix(point.Timestamp, "T00:00:00Z") {
			t.Fatalf("expected daily (midnight) buckets, got %s", point.Timestamp)
		}
	}
	total := 0
	for _, point := range series {
		total += point.Connections
	}
	if total != 30 {
		t.Fatalf("expected 30 connections across daily buckets, got %d", total)
	}
	// spark labels should be compact daily labels
	if len(payload.Sources) == 1 {
		for _, label := range payload.Sources[0].SparkHours {
			if !regexpDailyLabel.MatchString(label) {
				t.Fatalf("expected MM-DD spark label, got %q", label)
			}
		}
	}
}

func TestIntelHourlySeriesForShortWindows(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-20*time.Hour), 60, 200),
		intelEntryAt("10.0.0.5", "192.168.1.175:25565", "192.168.76.77:25565", now.Add(-2*time.Hour), 60, 200),
	}
	harness := newIntelHarness(entries, nil)
	_, payload := harness.serve("range=24h&live=false")
	series := payload.Overview.Hourly
	if len(series) < 20 || len(series) > 26 {
		t.Fatalf("expected ~24 hourly buckets, got %d", len(series))
	}
	for _, point := range series {
		if !strings.HasSuffix(point.Timestamp, ":00Z") {
			t.Fatalf("expected hourly buckets, got %s", point.Timestamp)
		}
	}
}

func TestIntelAllTimeDefaultLabel(t *testing.T) {
	harness := newIntelHarness([]streamLogEntry{}, nil)
	_, payload := harness.serve("live=false")
	if payload.Query.RangeLabel != "All time" {
		t.Fatalf("expected All time label by default, got %q", payload.Query.RangeLabel)
	}
}
