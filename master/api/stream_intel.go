package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"512SvMan/db"
	"512SvMan/dnsmasq"
	"512SvMan/npm"

	"github.com/oschwald/geoip2-golang"
)

const (
	intelRecencyWindow         = 10 * time.Minute
	intelNewEntityWindow       = 24 * time.Hour
	intelLiveTTL               = 20 * time.Second
	intelMaxSources            = 2000
	intelMaxRoutes             = 2000
	intelMaxDestinations       = 1000
	intelMaxSeriesPoints       = 400
	intelSparkMaxBuckets       = 30
	intelDailyStepThreshold    = 72 * time.Hour
	intelMaxSourceDests        = 50
	intelMaxSourceRoutes       = 50
	intelMaxRouteSources       = 100
	intelMaxDestinationSources = 100
	intelMaxDestinationRoutes  = 100
	intelDefaultSessionLimit   = 100
	intelMaxSessionLimit       = 500
	intelLongSessionSeconds    = 4 * 3600
	intelSpikeBaselineBuckets  = 24
	intelSpikeMinBaseline      = 6
	intelSpikeZScore           = 2.5
	intelSpikeCriticalZScore   = 4.0
	intelMinSpikeConnections   = 5
	intelFailureBurstMin       = 10
	intelFailureBurstRate      = 0.3
	intelUnusualSourceDests    = 10
	intelUnusualFailureRate    = 0.5
	intelUnusualFailureMin     = 10
	intelMaxInsights           = 40
	intelMaxNewIPInsights      = 10
	intelMaxTopEntities        = 8
	intelMaxLongestSessions    = 8
	intelMaxRecentSessions     = 12
	intelMaxSearchMatches      = 25
)

type intelDependencies struct {
	loadEntries     func() ([]streamLogEntry, *geoip2.Reader, error)
	getAliases      func() ([]dnsmasq.AliasEntry, error)
	listStreams     func(string, string) ([]npm.Stream, error)
	getDescriptions func(context.Context, string, []int) (map[int]string, error)
	captureLive     func(context.Context, string) (*liveSnapshot, error)
	now             func() time.Time
}

var productionIntelDependencies = intelDependencies{
	loadEntries:     loadEntriesWithGeoIP,
	getAliases:      dnsmasq.GetAllAliases,
	listStreams:     npm.ListStreams,
	getDescriptions: db.GetResourceDescriptions,
	captureLive: func(ctx context.Context, authToken string) (*liveSnapshot, error) {
		return captureLiveSnapshot(ctx, authToken, productionLiveDependencies, intelLiveTTL)
	},
	now: time.Now,
}

type intelRef struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Count int    `json:"count,omitempty"`
}

type intelSource struct {
	IP                 string     `json:"ip"`
	Aliases            []string   `json:"aliases"`
	Scope              string     `json:"scope"`
	Countries          []string   `json:"countries"`
	ActiveNow          bool       `json:"active_now"`
	ActiveConnections  int        `json:"active_connections"`
	RecentlyActive     bool       `json:"recently_active"`
	New                bool       `json:"new"`
	FirstSeen          string     `json:"first_seen"`
	LastSeen           string     `json:"last_seen"`
	Connections        int        `json:"connections"`
	Failed             int        `json:"failed"`
	FailureRate        float64    `json:"failure_rate"`
	BytesSent          int64      `json:"bytes_sent"`
	BytesReceived      int64      `json:"bytes_received"`
	TotalBytes         int64      `json:"total_bytes"`
	AvgSession         float64    `json:"avg_session_seconds"`
	MaxSession         float64    `json:"max_session_seconds"`
	UniqueDestinations int        `json:"unique_destinations"`
	UniqueRoutes       int        `json:"unique_routes"`
	UniqueListeners    int        `json:"unique_listeners"`
	Destinations       []intelRef `json:"destinations"`
	Routes             []intelRef `json:"routes"`
	Listeners          []intelRef `json:"listeners"`
	Ports              []string   `json:"ports"`
	Protocols          []string   `json:"protocols"`
	Spark              []int      `json:"spark"`
	SparkHours         []string   `json:"spark_hours"`
	Signals            []string   `json:"signals"`
	LastDestination    string     `json:"last_destination"`
}

type intelRoute struct {
	ID                string            `json:"id"`
	Listener          analyticsEndpoint `json:"listener"`
	Destination       analyticsEndpoint `json:"destination"`
	Protocol          string            `json:"protocol"`
	StreamMatchStatus string            `json:"stream_match_status"`
	Streams           []analyticsStream `json:"streams"`
	ActiveNow         bool              `json:"active_now"`
	ActiveConnections int               `json:"active_connections"`
	SourceCount       int               `json:"source_count"`
	SourceIPs         []string          `json:"source_ips"`
	FirstSeen         string            `json:"first_seen"`
	LastSeen          string            `json:"last_seen"`
	Connections       int               `json:"connections"`
	Failed            int               `json:"failed"`
	FailureRate       float64           `json:"failure_rate"`
	TotalBytes        int64             `json:"total_bytes"`
	AvgSession        float64           `json:"avg_session_seconds"`
	MaxSession        float64           `json:"max_session_seconds"`
	Spark             []int             `json:"spark"`
	SparkHours        []string          `json:"spark_hours"`
	Signals           []string          `json:"signals"`
}

type intelDestination struct {
	Endpoint          analyticsEndpoint `json:"endpoint"`
	ActiveNow         bool              `json:"active_now"`
	ActiveConnections int               `json:"active_connections"`
	UniqueSources     int               `json:"unique_sources"`
	TopSources        []intelRef        `json:"top_sources"`
	Routes            []intelRef        `json:"routes"`
	Protocols         []string          `json:"protocols"`
	Countries         []string          `json:"countries"`
	FirstSeen         string            `json:"first_seen"`
	LastSeen          string            `json:"last_seen"`
	Connections       int               `json:"connections"`
	Failed            int               `json:"failed"`
	FailureRate       float64           `json:"failure_rate"`
	TotalBytes        int64             `json:"total_bytes"`
	AvgSession        float64           `json:"avg_session_seconds"`
	MaxSession        float64           `json:"max_session_seconds"`
	Spark             []int             `json:"spark"`
	SparkHours        []string          `json:"spark_hours"`
	Signals           []string          `json:"signals"`
}

type intelSession struct {
	Timestamp         string            `json:"timestamp"`
	EndedAt           string            `json:"ended_at"`
	Source            analyticsEndpoint `json:"source"`
	ObservedListener  analyticsEndpoint `json:"observed_listener"`
	Destination       analyticsEndpoint `json:"destination"`
	Protocol          string            `json:"protocol"`
	Country           string            `json:"country"`
	RouteID           string            `json:"route_id"`
	StreamMatchStatus string            `json:"stream_match_status"`
	Streams           []analyticsStream `json:"streams"`
	Status            int               `json:"status"`
	Outcome           string            `json:"outcome"`
	BytesSent         int64             `json:"bytes_sent"`
	BytesReceived     int64             `json:"bytes_received"`
	TotalBytes        int64             `json:"total_bytes"`
	SessionSeconds    float64           `json:"session_seconds"`
}

type intelLiveSummary struct {
	Available   bool   `json:"available"`
	Partial     bool   `json:"partial"`
	CapturedAt  string `json:"captured_at"`
	Total       int    `json:"total"`
	Established int    `json:"established"`
	Listening   int    `json:"listening"`
	Handshake   int    `json:"handshake"`
	Closing     int    `json:"closing"`
}

type intelNewIP struct {
	IP          string `json:"ip"`
	FirstSeen   string `json:"first_seen"`
	Connections int    `json:"connections"`
	Country     string `json:"country"`
}

type intelSpike struct {
	Timestamp   string  `json:"timestamp"`
	Kind        string  `json:"kind"`
	Connections int     `json:"connections"`
	Failed      int     `json:"failed"`
	FailureRate float64 `json:"failure_rate"`
	Baseline    float64 `json:"baseline_mean"`
	ZScore      float64 `json:"z_score"`
	Severity    string  `json:"severity"`
}

type intelWindow struct {
	Connections   int `json:"connections"`
	Failed        int `json:"failed"`
	UniqueSources int `json:"unique_sources"`
	UniqueRoutes  int `json:"unique_routes"`
	NewIPs        int `json:"new_ips"`
}

type intelOverview struct {
	Live             intelLiveSummary `json:"live"`
	Hourly           []intelHourPoint `json:"hourly"`
	ActiveIPs        []string         `json:"active_ips"`
	ActiveRoutes     []string         `json:"active_routes"`
	NewIPs           []intelNewIP     `json:"new_ips"`
	TopSources       []intelRef       `json:"top_sources"`
	TopDestinations  []intelRef       `json:"top_destinations"`
	TopRoutes        []intelRef       `json:"top_routes"`
	LongestSessions  []intelSession   `json:"longest_sessions"`
	RecentSessions   []intelSession   `json:"recent_sessions"`
	Spikes           []intelSpike     `json:"spikes"`
	Window           intelWindow      `json:"window"`
	TotalConnections int              `json:"total_connections"`
}

type intelInsight struct {
	ID        string `json:"id"`
	Severity  string `json:"severity"`
	Category  string `json:"category"`
	Title     string `json:"title"`
	Detail    string `json:"detail"`
	Link      string `json:"link"`
	Timestamp string `json:"timestamp"`
}

type intelProfile struct {
	Kind         string            `json:"kind"`
	Source       *intelSource      `json:"source,omitempty"`
	Route        *intelRoute       `json:"route,omitempty"`
	Destination  *intelDestination `json:"destination,omitempty"`
	Hourly       []intelHourPoint  `json:"hourly"`
	ActivityFrom string            `json:"activity_from"`
}

type intelHourPoint struct {
	Timestamp   string `json:"timestamp"`
	Connections int    `json:"connections"`
	Failed      int    `json:"failed"`
	TotalBytes  int64  `json:"total_bytes"`
}

type intelSearchMatches struct {
	Sources      []intelSource      `json:"sources"`
	Routes       []intelRoute       `json:"routes"`
	Destinations []intelDestination `json:"destinations"`
	Query        string             `json:"query"`
	Interpreted  string             `json:"interpreted_as"`
}

type intelQuery struct {
	Search           string `json:"search"`
	SourceIP         string `json:"source_ip,omitempty"`
	ListenerIP       string `json:"listener_ip,omitempty"`
	DestinationIP    string `json:"destination_ip,omitempty"`
	RouteID          string `json:"route_id,omitempty"`
	DestinationExact string `json:"destination_exact,omitempty"`
	Start            string `json:"start,omitempty"`
	End              string `json:"end,omitempty"`
	Protocol         string `json:"protocol,omitempty"`
	Country          string `json:"country,omitempty"`
	Outcome          string `json:"outcome,omitempty"`
	NPMOnly          bool   `json:"npm_only"`
	Range            string `json:"range,omitempty"`
	RangeLabel       string `json:"range_label,omitempty"`
	WindowStart      string `json:"window_start,omitempty"`
	WindowEnd        string `json:"window_end,omitempty"`
}

type intelResponse struct {
	GeneratedAt     string                `json:"generated_at"`
	Now             string                `json:"now"`
	Query           intelQuery            `json:"query"`
	Mode            string                `json:"mode"`
	Live            intelLiveSummary      `json:"live"`
	Overview        *intelOverview        `json:"overview,omitempty"`
	Sources         []intelSource         `json:"sources"`
	Routes          []intelRoute          `json:"routes"`
	Destinations    []intelDestination    `json:"destinations"`
	Sessions        []intelSession        `json:"sessions"`
	SessionsTotal   int                   `json:"sessions_total"`
	Insights        []intelInsight        `json:"insights"`
	Profile         *intelProfile         `json:"profile,omitempty"`
	SearchMatches   *intelSearchMatches   `json:"search_matches,omitempty"`
	TotalEntries    int                   `json:"total_available_entries"`
	FilteredEntries int                   `json:"filtered_entries"`
	Availability    analyticsAvailability `json:"availability"`
	Warnings        []string              `json:"warnings"`
}

type intelSourceAgg struct {
	ip           string
	metrics      analyticsAccumulator
	destinations map[string]int
	routes       map[string]int
	listeners    map[string]int
	ports        map[string]struct{}
	protocols    map[string]struct{}
	countries    map[string]struct{}
	hourly       map[time.Time]*intelHourPoint
}

type intelRouteAgg struct {
	key         string
	listenerRaw string
	destRaw     string
	protocol    string
	metrics     analyticsAccumulator
	sources     map[string]int
	hourly      map[time.Time]*intelHourPoint
}

type intelDestinationAgg struct {
	raw       string
	metrics   analyticsAccumulator
	sources   map[string]int
	listeners map[string]int
	protocols map[string]struct{}
	countries map[string]struct{}
	hourly    map[time.Time]*intelHourPoint
}

func getStreamIntel(w http.ResponseWriter, r *http.Request) {
	serveStreamIntel(w, r, productionIntelDependencies)
}

func intelRouteKey(listener, destination, protocol string) string {
	return strings.Join([]string{listener, destination, strings.ToUpper(protocol)}, "\x00")
}

func intelRouteID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "route_" + hex.EncodeToString(sum[:6])
}

func intelValidRouteID(value string) bool {
	if len(value) != len("route_")+12 || !strings.HasPrefix(value, "route_") {
		return false
	}
	for _, char := range value[len("route_"):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func intelEndpointKey(ip, port string) string {
	return ip + "|" + port
}

func serveStreamIntel(w http.ResponseWriter, r *http.Request, deps intelDependencies) {
	now := deps.now().UTC()
	timeWindow, err := parseIntelTimeWindow(r.URL.Query(), now)
	if err != nil {
		respondJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	sanitizedQuery := r.URL.Query()
	sanitizedQuery.Del("start")
	sanitizedQuery.Del("end")
	sanitizedQuery.Del("range")
	sanitized := r.Clone(r.Context())
	sanitized.URL = &url.URL{Path: r.URL.Path, RawQuery: sanitizedQuery.Encode()}
	filters, err := parseAnalyticsFilters(sanitized)
	if err != nil {
		respondJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	filters.start = timeWindow.start
	filters.endExclusive = timeWindow.end
	query := r.URL.Query()
	search := strings.TrimSpace(query.Get("q"))
	npmOnly := strings.ToLower(strings.TrimSpace(query.Get("npm_only"))) == "true" || strings.TrimSpace(query.Get("npm_only")) == "1"
	routeID := strings.TrimSpace(query.Get("route_id"))
	destinationExact := strings.TrimSpace(query.Get("destination_exact"))
	if routeID != "" && !intelValidRouteID(routeID) {
		respondJSONError(w, http.StatusBadRequest, "route_id must use route_ followed by 12 lowercase hexadecimal characters")
		return
	}
	sessionLimit := intelDefaultSessionLimit
	if value := strings.TrimSpace(query.Get("session_limit")); value != "" {
		parsed, convErr := strconv.Atoi(value)
		if convErr != nil || parsed < 1 {
			respondJSONError(w, http.StatusBadRequest, "session_limit must be a positive integer")
			return
		}
		if parsed > intelMaxSessionLimit {
			parsed = intelMaxSessionLimit
		}
		sessionLimit = parsed
	}
	includeLive := true
	if value := strings.ToLower(strings.TrimSpace(query.Get("live"))); value == "false" || value == "0" {
		includeLive = false
	}

	response := intelResponse{
		Now:          now.Format(time.RFC3339),
		Mode:         "overview",
		Sources:      make([]intelSource, 0),
		Routes:       make([]intelRoute, 0),
		Destinations: make([]intelDestination, 0),
		Sessions:     make([]intelSession, 0),
		Insights:     make([]intelInsight, 0),
		Warnings:     make([]string, 0),
		Availability: analyticsAvailability{GeoIP: false, DNSAliases: false, NPMStreams: false, Descriptions: false},
	}
	response.Query = intelQuery{
		Search: search, SourceIP: filters.sourceIP, ListenerIP: filters.listenerIP,
		DestinationIP: filters.destinationIP, RouteID: routeID, DestinationExact: destinationExact,
		Protocol: filters.protocol, Country: filters.country, Outcome: filters.outcome,
		Range: timeWindow.token, RangeLabel: timeWindow.label, NPMOnly: npmOnly,
	}
	if !timeWindow.start.IsZero() {
		response.Query.Start = timeWindow.start.UTC().Format("2006-01-02")
		response.Query.WindowStart = timeWindow.start.UTC().Format(time.RFC3339)
	}
	if !timeWindow.end.IsZero() {
		response.Query.End = timeWindow.end.UTC().Add(-time.Second).Format("2006-01-02")
		response.Query.WindowEnd = timeWindow.end.UTC().Format(time.RFC3339)
	}

	entries, geoIPDB, err := deps.loadEntries()
	if geoIPDB != nil {
		defer geoIPDB.Close()
		response.Availability.GeoIP = geoIPDB != nil
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			respondJSONError(w, http.StatusNotFound, "stream-proxy.log not found")
			return
		}
		respondJSONError(w, http.StatusInternalServerError, "failed to load stream telemetry")
		return
	}
	response.TotalEntries = len(entries)

	aliasEntries, aliasErr := deps.getAliases()
	aliases := buildAnalyticsAliasMap(aliasEntries)
	response.Availability.DNSAliases = aliasErr == nil
	if aliasErr != nil {
		aliases = make(map[string][]string)
		response.Warnings = append(response.Warnings, "managed DNS aliases are unavailable")
	}

	streams, streamErr := deps.listStreams(baseURL, GetTokenFromContext(r))
	response.Availability.NPMStreams = streamErr == nil
	descriptions := make(map[int]string)
	if streamErr != nil {
		streams = []npm.Stream{}
		response.Warnings = append(response.Warnings, "NPM stream definitions are unavailable")
	} else {
		ids := make([]int, len(streams))
		for i := range streams {
			ids[i] = streams[i].ID
		}
		if descriptions, err = deps.getDescriptions(r.Context(), "npm_stream", ids); err != nil {
			descriptions = make(map[int]string)
			response.Warnings = append(response.Warnings, "NPM stream descriptions are unavailable")
		} else {
			response.Availability.Descriptions = true
		}
	}
	streamsAvailable := streamErr == nil
	if npmOnly && streamsAvailable {
		entries = filterIntelNPMEntries(entries, aliases, streams, descriptions)
	}

	entries = filterAnalyticsEntries(entries, filters, aliases)
	if search != "" {
		entries = intelFilterBySearch(entries, search, aliases)
	}
	if routeID != "" {
		entries = intelFilterByRoute(entries, routeID)
	}
	if destinationExact != "" {
		entries = intelFilterByDestinationExact(entries, destinationExact)
	}
	response.FilteredEntries = len(entries)

	var snapshot *liveSnapshot
	if includeLive {
		snapshot, err = deps.captureLive(r.Context(), GetTokenFromContext(r))
		if err != nil {
			response.Warnings = append(response.Warnings, "live socket snapshot is unavailable; right-now states degrade to historical evidence")
		}
	}
	liveRemoteSources, liveDestSockets := intelLiveIndex(snapshot, streamsAvailable)
	response.Live = intelSummarizeLive(snapshot, liveRemoteSources, liveDestSockets)

	sourceAggs, routeAggs, destAggs, globalHourly := intelAggregate(entries)
	sessions := intelBuildSessions(entries, aliases, streams, descriptions, streamsAvailable)

	dataStart, dataEnd := intelHourlyBounds(globalHourly)
	axisStart, axisEnd := timeWindow.start, timeWindow.end
	if axisStart.IsZero() {
		axisStart = dataStart
	}
	if axisEnd.IsZero() {
		axisEnd = dataEnd
		if !dataEnd.IsZero() {
			axisEnd = dataEnd.Add(time.Hour)
		}
	}
	if !dataStart.IsZero() && response.Query.WindowStart == "" {
		response.Query.WindowStart = dataStart.UTC().Format(time.RFC3339)
	}
	if !dataEnd.IsZero() && response.Query.WindowEnd == "" {
		response.Query.WindowEnd = dataEnd.Add(time.Hour).UTC().Format(time.RFC3339)
	}
	denseAxis := intelBuildAxis(axisStart, axisEnd, intelMaxSeriesPoints)
	sparkAxis := intelSparkAxis(denseAxis)

	profileMode := "overview"
	switch {
	case filters.sourceIP != "":
		profileMode = "source"
	case routeID != "":
		profileMode = "route"
	case destinationExact != "":
		profileMode = "destination"
	case search != "":
		profileMode = "search"
	}
	response.Mode = profileMode

	response.Sources = intelBuildSources(sourceAggs, aliases, liveRemoteSources, now, sparkAxis, intelMaxSources)
	response.Routes = intelBuildRoutes(routeAggs, aliases, streams, descriptions, streamsAvailable, liveDestSockets, now, sparkAxis, intelMaxRoutes)
	response.Destinations = intelBuildDestinations(destAggs, aliases, liveDestSockets, now, sparkAxis, intelMaxDestinations)

	sessionsAvailable := len(sessions)
	if sessionsAvailable > sessionLimit {
		sessions = sessions[:sessionLimit]
	}
	response.Sessions = sessions
	response.SessionsTotal = sessionsAvailable

	switch profileMode {
	case "source":
		response.Profile = intelSourceProfile(sourceAggs, filters.sourceIP, aliases, liveRemoteSources, now, sparkAxis, denseAxis)
		if response.Profile == nil {
			respondJSONError(w, http.StatusNotFound, "no telemetry for source IP in the applied window")
			return
		}
	case "route":
		response.Profile = intelRouteProfile(routeAggs, routeID, aliases, streams, descriptions, streamsAvailable, liveDestSockets, now, sparkAxis, denseAxis)
		if response.Profile == nil {
			respondJSONError(w, http.StatusNotFound, "no telemetry for route in the applied window")
			return
		}
	case "destination":
		response.Profile = intelDestinationProfile(destAggs, destinationExact, aliases, liveDestSockets, now, sparkAxis, denseAxis)
		if response.Profile == nil {
			respondJSONError(w, http.StatusNotFound, "no telemetry for destination in the applied window")
			return
		}
	case "search":
		response.SearchMatches = intelSearchEntityMatches(search, response.Sources, response.Routes, response.Destinations)
	}

	if profileMode == "overview" {
		response.Overview = intelBuildOverview(entries, sessions, sourceAggs, routeAggs, destAggs, globalHourly, denseAxis, response.Live, liveRemoteSources, liveDestSockets, now, aliases)
		response.Insights = intelBuildInsights(sourceAggs, routeAggs, globalHourly, sessions, now)
	}

	response.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func filterIntelNPMEntries(entries []streamLogEntry, aliases map[string][]string, streams []npm.Stream, descriptions map[int]string) []streamLogEntry {
	filtered := make([]streamLogEntry, 0, len(entries))
	for _, entry := range entries {
		listener := enrichAnalyticsListener(entry.ProxyAddr, aliases)
		destination := enrichAnalyticsEndpoint(entry.UpstreamAddr, aliases)
		matched, _ := matchAnalyticsStreams(listener, destination, entry.Protocol, aliases, streams, descriptions, true)
		if len(matched) > 0 {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func intelFilterBySearch(entries []streamLogEntry, search string, aliases map[string][]string) []streamLogEntry {
	needle := strings.ToLower(search)
	exactIP := ""
	if ip := net.ParseIP(search); ip != nil {
		exactIP = ip.String()
	}
	filtered := make([]streamLogEntry, 0, len(entries))
	for _, entry := range entries {
		if exactIP != "" {
			if parseAnalyticsEndpoint(entry.ClientIP).ip == exactIP ||
				parseAnalyticsListener(entry.ProxyAddr).ip == exactIP ||
				parseAnalyticsEndpoint(entry.UpstreamAddr).ip == exactIP {
				filtered = append(filtered, entry)
			}
			continue
		}
		source := enrichAnalyticsEndpoint(entry.ClientIP, aliases)
		listener := enrichAnalyticsListener(entry.ProxyAddr, aliases)
		destination := enrichAnalyticsEndpoint(entry.UpstreamAddr, aliases)
		if analyticsEndpointMatches(source, search) || analyticsEndpointMatches(listener, search) ||
			analyticsEndpointMatches(destination, search) || analyticsContains(entry.Protocol, needle) ||
			analyticsContains(analyticsCountry(entry.Country), needle) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func intelFilterByRoute(entries []streamLogEntry, routeID string) []streamLogEntry {
	filtered := make([]streamLogEntry, 0, len(entries))
	for _, entry := range entries {
		if intelRouteID(analyticsPathKey(entry)) == routeID || intelRouteID(intelRouteKey(entry.ProxyAddr, entry.UpstreamAddr, entry.Protocol)) == routeID {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func intelFilterByDestinationExact(entries []streamLogEntry, destination string) []streamLogEntry {
	filtered := make([]streamLogEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.UpstreamAddr == destination {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func intelLiveIndex(snapshot *liveSnapshot, streamsAvailable bool) (map[string]int, map[string]int) {
	sources := make(map[string]int)
	destinations := make(map[string]int)
	if snapshot == nil {
		return sources, destinations
	}
	for _, connection := range snapshot.connections {
		if connection.State != "established" {
			continue
		}
		remote := connection.Remote
		if remote.IP == "" {
			continue
		}
		isInbound := connection.Correlation.Role == "inbound_listener"
		isOutbound := connection.Correlation.Role == "outbound_upstream"
		if streamsAvailable && isInbound {
			sources[remote.IP]++
		}
		if streamsAvailable && isOutbound {
			destinations[intelEndpointKey(remote.IP, remote.Port)]++
			continue
		}
		if !streamsAvailable {
			sources[remote.IP]++
			destinations[intelEndpointKey(remote.IP, remote.Port)]++
		}
	}
	return sources, destinations
}

func intelSummarizeLive(snapshot *liveSnapshot, sources, destinations map[string]int) intelLiveSummary {
	summary := intelLiveSummary{}
	if snapshot == nil {
		return summary
	}
	summary.Available = snapshot.availability.Available
	summary.Partial = snapshot.availability.Partial
	summary.CapturedAt = snapshot.capturedAt.Format(time.RFC3339)
	for _, connection := range snapshot.connections {
		summary.Total++
		switch connection.StateGroup {
		case "active":
			summary.Established++
		case "listening":
			summary.Listening++
		case "handshake":
			summary.Handshake++
		case "closing":
			summary.Closing++
		}
	}
	return summary
}

func intelAggregate(entries []streamLogEntry) (map[string]*intelSourceAgg, map[string]*intelRouteAgg, map[string]*intelDestinationAgg, map[time.Time]*intelHourPoint) {
	sourceAggs := make(map[string]*intelSourceAgg)
	routeAggs := make(map[string]*intelRouteAgg)
	destAggs := make(map[string]*intelDestinationAgg)
	globalHourly := make(map[time.Time]*intelHourPoint)

	addHour := func(target map[time.Time]*intelHourPoint, hour time.Time, entry streamLogEntry) {
		bucket := target[hour]
		if bucket == nil {
			bucket = &intelHourPoint{Timestamp: hour.Format(time.RFC3339)}
			target[hour] = bucket
		}
		bucket.Connections++
		if entry.Status != http.StatusOK {
			bucket.Failed++
		}
		bucket.TotalBytes += entry.BytesSent + entry.BytesReceived
	}

	for _, entry := range entries {
		sourceIP := analyticsSourceIdentity(entry.ClientIP)
		source := sourceAggs[sourceIP]
		if source == nil {
			source = &intelSourceAgg{
				ip: sourceIP, destinations: make(map[string]int), routes: make(map[string]int),
				listeners: make(map[string]int), ports: make(map[string]struct{}),
				protocols: make(map[string]struct{}), countries: make(map[string]struct{}),
				hourly: make(map[time.Time]*intelHourPoint),
			}
			sourceAggs[sourceIP] = source
		}
		addAnalyticsMetrics(&source.metrics, entry)
		source.destinations[entry.UpstreamAddr]++
		source.listeners[entry.ProxyAddr]++
		if port := analyticsPort(entry.UpstreamAddr); port != "unknown" {
			source.ports[port] = struct{}{}
		}
		source.protocols[strings.ToUpper(entry.Protocol)] = struct{}{}
		source.countries[analyticsCountry(entry.Country)] = struct{}{}

		routeKey := intelRouteKey(entry.ProxyAddr, entry.UpstreamAddr, entry.Protocol)
		source.routes[intelRouteID(routeKey)]++
		route := routeAggs[routeKey]
		if route == nil {
			route = &intelRouteAgg{
				key: routeKey, listenerRaw: entry.ProxyAddr, destRaw: entry.UpstreamAddr,
				protocol: strings.ToUpper(entry.Protocol), sources: make(map[string]int),
				hourly: make(map[time.Time]*intelHourPoint),
			}
			routeAggs[routeKey] = route
		}
		addAnalyticsMetrics(&route.metrics, entry)
		route.sources[sourceIP]++

		destination := destAggs[entry.UpstreamAddr]
		if destination == nil {
			destination = &intelDestinationAgg{
				raw: entry.UpstreamAddr, sources: make(map[string]int), listeners: make(map[string]int),
				protocols: make(map[string]struct{}), countries: make(map[string]struct{}),
				hourly: make(map[time.Time]*intelHourPoint),
			}
			destAggs[entry.UpstreamAddr] = destination
		}
		addAnalyticsMetrics(&destination.metrics, entry)
		destination.sources[sourceIP]++
		destination.listeners[entry.ProxyAddr]++
		destination.protocols[strings.ToUpper(entry.Protocol)] = struct{}{}
		destination.countries[analyticsCountry(entry.Country)] = struct{}{}

		if !entry.Time.IsZero() {
			hour := entry.Time.UTC().Truncate(time.Hour)
			addHour(source.hourly, hour, entry)
			addHour(route.hourly, hour, entry)
			addHour(destination.hourly, hour, entry)
			addHour(globalHourly, hour, entry)
		}
	}
	return sourceAggs, routeAggs, destAggs, globalHourly
}

type intelTimeWindow struct {
	start time.Time
	end   time.Time
	token string
	label string
}

func intelRangeForToken(token string, now time.Time) (time.Time, time.Time, string, bool) {
	if token == "all" {
		return time.Time{}, time.Time{}, "All time", true
	}
	ranges := map[string]struct {
		duration time.Duration
		label    string
	}{
		"24h":  {24 * time.Hour, "Last 24 hours"},
		"1d":   {24 * time.Hour, "Last 24 hours"},
		"7d":   {7 * 24 * time.Hour, "Last 7 days"},
		"14d":  {14 * 24 * time.Hour, "Last 14 days"},
		"1mo":  {30 * 24 * time.Hour, "Last 30 days"},
		"30d":  {30 * 24 * time.Hour, "Last 30 days"},
		"3mo":  {90 * 24 * time.Hour, "Last 90 days"},
		"90d":  {90 * 24 * time.Hour, "Last 90 days"},
		"6mo":  {180 * 24 * time.Hour, "Last 180 days"},
		"180d": {180 * 24 * time.Hour, "Last 180 days"},
		"1y":   {365 * 24 * time.Hour, "Last year"},
		"365d": {365 * 24 * time.Hour, "Last year"},
	}
	window, ok := ranges[token]
	if !ok {
		return time.Time{}, time.Time{}, "", false
	}
	return now.Add(-window.duration), now, window.label, true
}

func parseIntelTimeValue(raw string, endOfDay bool) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, true
	}
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		if endOfDay {
			return parsed.AddDate(0, 0, 1), true
		}
		return parsed, true
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02T15:04:05"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func parseIntelTimeWindow(query url.Values, now time.Time) (intelTimeWindow, error) {
	token := strings.ToLower(strings.TrimSpace(query.Get("range")))
	window := intelTimeWindow{token: "all", label: "All time"}
	if token != "" {
		start, end, label, ok := intelRangeForToken(token, now)
		if !ok {
			return intelTimeWindow{}, errors.New("range must be one of 24h, 7d, 14d, 1mo, 3mo, 6mo, 1y, all")
		}
		window.start, window.end, window.token, window.label = start, end, token, label
		return window, nil
	}
	startRaw := strings.TrimSpace(query.Get("start"))
	endRaw := strings.TrimSpace(query.Get("end"))
	if startRaw == "" && endRaw == "" {
		return window, nil
	}
	start, ok := parseIntelTimeValue(startRaw, false)
	if !ok {
		return intelTimeWindow{}, errors.New("start must use YYYY-MM-DD, YYYY-MM-DDTHH:MM, or RFC3339")
	}
	end, ok := parseIntelTimeValue(endRaw, true)
	if !ok {
		return intelTimeWindow{}, errors.New("end must use YYYY-MM-DD, YYYY-MM-DDTHH:MM, or RFC3339")
	}
	if !start.IsZero() && !end.IsZero() && !start.Before(end) {
		return intelTimeWindow{}, errors.New("start must be before end")
	}
	window.start, window.end = start.UTC(), end.UTC()
	window.token, window.label = "custom", "Custom range"
	return window, nil
}

type intelAxis struct {
	buckets []time.Time
	step    time.Duration
}

func intelBuildAxis(start, end time.Time, maxPoints int) intelAxis {
	axis := intelAxis{}
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return axis
	}
	span := end.Sub(start)
	step := time.Hour
	if span > intelDailyStepThreshold {
		step = 24 * time.Hour
	}
	count := int(span/step) + 1
	if count > maxPoints {
		factor := (count + maxPoints - 1) / maxPoints
		step *= time.Duration(factor)
	}
	buckets := make([]time.Time, 0, count)
	for bucket := start.Truncate(step); !bucket.After(end); bucket = bucket.Add(step) {
		buckets = append(buckets, bucket)
	}
	axis.buckets = buckets
	axis.step = step
	return axis
}

func intelSparkAxis(axis intelAxis) intelAxis {
	if len(axis.buckets) > intelSparkMaxBuckets {
		axis.buckets = append([]time.Time(nil), axis.buckets[len(axis.buckets)-intelSparkMaxBuckets:]...)
	}
	return axis
}

func intelHourlyBounds(hourly map[time.Time]*intelHourPoint) (time.Time, time.Time) {
	var min, max time.Time
	for hour := range hourly {
		if min.IsZero() || hour.Before(min) {
			min = hour
		}
		if max.IsZero() || hour.After(max) {
			max = hour
		}
	}
	return min, max
}

func intelSparkLabelLayout(step time.Duration) string {
	if step >= 24*time.Hour {
		return "01-02"
	}
	return "15:04"
}

func intelSpark(hourly map[time.Time]*intelHourPoint, axis intelAxis) ([]int, []string) {
	values := make([]int, len(axis.buckets))
	labels := make([]string, len(axis.buckets))
	if len(axis.buckets) == 0 {
		return values, labels
	}
	counts := make(map[time.Time]int, len(hourly))
	for hour, bucket := range hourly {
		counts[hour.Truncate(axis.step)] += bucket.Connections
	}
	layout := intelSparkLabelLayout(axis.step)
	for i, bucket := range axis.buckets {
		labels[i] = bucket.Format(layout)
		values[i] = counts[bucket]
	}
	return values, labels
}

func intelHourlySeries(hourly map[time.Time]*intelHourPoint, axis intelAxis) []intelHourPoint {
	result := make([]intelHourPoint, 0, len(axis.buckets))
	if len(axis.buckets) == 0 {
		return result
	}
	aggregated := make(map[time.Time]*intelHourPoint, len(axis.buckets))
	for _, bucket := range axis.buckets {
		aggregated[bucket] = &intelHourPoint{Timestamp: bucket.Format(time.RFC3339)}
	}
	for hour, point := range hourly {
		bucket := aggregated[hour.Truncate(axis.step)]
		if bucket == nil {
			continue
		}
		bucket.Connections += point.Connections
		bucket.Failed += point.Failed
		bucket.TotalBytes += point.TotalBytes
	}
	for _, bucket := range axis.buckets {
		result = append(result, *aggregated[bucket])
	}
	return result
}

func intelSourceSignals(agg *intelSourceAgg, active bool, now time.Time) []string {
	signals := make([]string, 0)
	if !agg.metrics.firstSeen.IsZero() && now.Sub(agg.metrics.firstSeen) <= intelNewEntityWindow {
		signals = append(signals, "new_ip")
	}
	if len(agg.destinations) >= intelUnusualSourceDests {
		signals = append(signals, "many_destinations")
	}
	if agg.metrics.maxSession >= intelLongSessionSeconds {
		signals = append(signals, "long_session")
	}
	if agg.metrics.connections >= intelUnusualFailureMin && analyticsFailureRate(agg.metrics) >= intelUnusualFailureRate {
		signals = append(signals, "failure_burst")
	}
	return signals
}

func intelBuildSources(aggs map[string]*intelSourceAgg, aliases map[string][]string, liveSources map[string]int, now time.Time, sparkAxis intelAxis, limit int) []intelSource {
	result := make([]intelSource, 0, len(aggs))
	for _, agg := range aggs {
		endpoint := enrichAnalyticsEndpoint(agg.ip, aliases)
		lastSeen := analyticsTime(agg.metrics.lastSeen)
		recent := agg.metrics.lastSeen.IsZero() == false && now.Sub(agg.metrics.lastSeen) <= intelRecencyWindow
		isNew := agg.metrics.firstSeen.IsZero() == false && now.Sub(agg.metrics.firstSeen) <= intelNewEntityWindow
		spark, sparkLabels := intelSpark(agg.hourly, sparkAxis)

		destinationRefs := make([]intelRef, 0, len(agg.destinations))
		for raw, count := range agg.destinations {
			destinationRefs = append(destinationRefs, intelRef{
				ID: raw, Kind: "destination",
				Label: intelShortLabel(enrichAnalyticsEndpoint(raw, aliases)), Count: count,
			})
		}
		sort.Slice(destinationRefs, func(i, j int) bool {
			if destinationRefs[i].Count != destinationRefs[j].Count {
				return destinationRefs[i].Count > destinationRefs[j].Count
			}
			return destinationRefs[i].ID < destinationRefs[j].ID
		})
		if len(destinationRefs) > intelMaxSourceDests {
			destinationRefs = destinationRefs[:intelMaxSourceDests]
		}

		routeRefs := make([]intelRef, 0, len(agg.routes))
		for id, count := range agg.routes {
			routeRefs = append(routeRefs, intelRef{ID: id, Kind: "route", Label: id, Count: count})
		}
		sort.Slice(routeRefs, func(i, j int) bool {
			if routeRefs[i].Count != routeRefs[j].Count {
				return routeRefs[i].Count > routeRefs[j].Count
			}
			return routeRefs[i].ID < routeRefs[j].ID
		})
		if len(routeRefs) > intelMaxSourceRoutes {
			routeRefs = routeRefs[:intelMaxSourceRoutes]
		}

		listenerRefs := make([]intelRef, 0, len(agg.listeners))
		for raw, count := range agg.listeners {
			listenerRefs = append(listenerRefs, intelRef{
				ID: raw, Kind: "listener",
				Label: intelShortLabel(enrichAnalyticsListener(raw, aliases)), Count: count,
			})
		}
		sort.Slice(listenerRefs, func(i, j int) bool {
			if listenerRefs[i].Count != listenerRefs[j].Count {
				return listenerRefs[i].Count > listenerRefs[j].Count
			}
			return listenerRefs[i].ID < listenerRefs[j].ID
		})

		lastDestination := ""
		if len(destinationRefs) > 0 {
			lastDestination = destinationRefs[0].Label
		}

		activeConnections := liveSources[agg.ip]
		result = append(result, intelSource{
			IP: agg.ip, Aliases: endpoint.Aliases, Scope: analyticsIPScope(agg.ip),
			Countries: sortedSet(agg.countries), ActiveNow: activeConnections > 0,
			ActiveConnections: activeConnections, RecentlyActive: recent, New: isNew,
			FirstSeen: analyticsTime(agg.metrics.firstSeen), LastSeen: lastSeen,
			Connections: agg.metrics.connections, Failed: agg.metrics.failures,
			FailureRate: analyticsFailureRate(agg.metrics), BytesSent: agg.metrics.bytesSent,
			BytesReceived: agg.metrics.bytesReceived, TotalBytes: agg.metrics.bytesSent + agg.metrics.bytesReceived,
			AvgSession: analyticsAverageSession(agg.metrics), MaxSession: agg.metrics.maxSession,
			UniqueDestinations: len(agg.destinations), UniqueRoutes: len(agg.routes),
			UniqueListeners: len(agg.listeners),
			Destinations:    destinationRefs, Routes: routeRefs, Listeners: listenerRefs,
			Ports: sortedSet(agg.ports), Protocols: sortedSet(agg.protocols),
			Spark: spark, SparkHours: sparkLabels,
			Signals: intelSourceSignals(agg, activeConnections > 0, now), LastDestination: lastDestination,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ActiveNow != result[j].ActiveNow {
			return result[i].ActiveNow
		}
		if result[i].Connections != result[j].Connections {
			return result[i].Connections > result[j].Connections
		}
		return result[i].IP < result[j].IP
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func intelShortLabel(endpoint analyticsEndpoint) string {
	if len(endpoint.Aliases) > 0 {
		return endpoint.Aliases[0]
	}
	if endpoint.IP != "" && endpoint.Port != "" {
		return net.JoinHostPort(endpoint.IP, endpoint.Port)
	}
	if endpoint.IP != "" {
		return endpoint.IP
	}
	return endpoint.RawAddress
}

func intelBuildRoutes(aggs map[string]*intelRouteAgg, aliases map[string][]string, streams []npm.Stream, descriptions map[int]string, streamsAvailable bool, liveDestinations map[string]int, now time.Time, sparkAxis intelAxis, limit int) []intelRoute {
	result := make([]intelRoute, 0, len(aggs))
	for _, agg := range aggs {
		listener := enrichAnalyticsListener(agg.listenerRaw, aliases)
		destination := enrichAnalyticsEndpoint(agg.destRaw, aliases)
		matchedStreams, matchStatus := matchAnalyticsStreams(listener, destination, agg.protocol, aliases, streams, descriptions, streamsAvailable)
		spark, sparkLabels := intelSpark(agg.hourly, sparkAxis)
		sourceIPs := make([]string, 0, len(agg.sources))
		for ip := range agg.sources {
			sourceIPs = append(sourceIPs, ip)
		}
		sort.Strings(sourceIPs)
		if len(sourceIPs) > intelMaxRouteSources {
			sourceIPs = sourceIPs[:intelMaxRouteSources]
		}
		activeConnections := liveDestinations[intelEndpointKey(destination.IP, destination.Port)]
		recent := agg.metrics.lastSeen.IsZero() == false && now.Sub(agg.metrics.lastSeen) <= intelRecencyWindow
		signals := make([]string, 0)
		if agg.metrics.maxSession >= intelLongSessionSeconds {
			signals = append(signals, "long_session")
		}
		if agg.metrics.connections >= intelUnusualFailureMin && analyticsFailureRate(agg.metrics) >= intelUnusualFailureRate {
			signals = append(signals, "failure_burst")
		}
		result = append(result, intelRoute{
			ID: intelRouteID(agg.key), Listener: listener, Destination: destination,
			Protocol: agg.protocol, StreamMatchStatus: matchStatus, Streams: matchedStreams,
			ActiveNow: activeConnections > 0, ActiveConnections: activeConnections,
			SourceCount: len(agg.sources), SourceIPs: sourceIPs,
			FirstSeen: analyticsTime(agg.metrics.firstSeen), LastSeen: analyticsTime(agg.metrics.lastSeen),
			Connections: agg.metrics.connections, Failed: agg.metrics.failures,
			FailureRate: analyticsFailureRate(agg.metrics),
			TotalBytes:  agg.metrics.bytesSent + agg.metrics.bytesReceived,
			AvgSession:  analyticsAverageSession(agg.metrics), MaxSession: agg.metrics.maxSession,
			Spark: spark, SparkHours: sparkLabels, Signals: signals,
		})
		_ = recent
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ActiveNow != result[j].ActiveNow {
			return result[i].ActiveNow
		}
		if result[i].Connections != result[j].Connections {
			return result[i].Connections > result[j].Connections
		}
		return result[i].ID < result[j].ID
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func intelBuildDestinations(aggs map[string]*intelDestinationAgg, aliases map[string][]string, liveDestinations map[string]int, now time.Time, sparkAxis intelAxis, limit int) []intelDestination {
	result := make([]intelDestination, 0, len(aggs))
	for _, agg := range aggs {
		endpoint := enrichAnalyticsEndpoint(agg.raw, aliases)
		spark, sparkLabels := intelSpark(agg.hourly, sparkAxis)
		topSources := make([]intelRef, 0, len(agg.sources))
		for ip, count := range agg.sources {
			topSources = append(topSources, intelRef{ID: ip, Kind: "source", Label: ip, Count: count})
		}
		sort.Slice(topSources, func(i, j int) bool {
			if topSources[i].Count != topSources[j].Count {
				return topSources[i].Count > topSources[j].Count
			}
			return topSources[i].ID < topSources[j].ID
		})
		if len(topSources) > intelMaxDestinationSources {
			topSources = topSources[:intelMaxDestinationSources]
		}
		routeRefs := make([]intelRef, 0, len(agg.listeners))
		for raw, count := range agg.listeners {
			routeRefs = append(routeRefs, intelRef{ID: intelRouteID(raw), Kind: "listener", Label: intelShortLabel(enrichAnalyticsListener(raw, aliases)), Count: count})
		}
		sort.Slice(routeRefs, func(i, j int) bool {
			if routeRefs[i].Count != routeRefs[j].Count {
				return routeRefs[i].Count > routeRefs[j].Count
			}
			return routeRefs[i].ID < routeRefs[j].ID
		})
		if len(routeRefs) > intelMaxDestinationRoutes {
			routeRefs = routeRefs[:intelMaxDestinationRoutes]
		}
		activeConnections := liveDestinations[intelEndpointKey(endpoint.IP, endpoint.Port)]
		signals := make([]string, 0)
		if agg.metrics.maxSession >= intelLongSessionSeconds {
			signals = append(signals, "long_session")
		}
		if agg.metrics.connections >= intelUnusualFailureMin && analyticsFailureRate(agg.metrics) >= intelUnusualFailureRate {
			signals = append(signals, "failure_burst")
		}
		result = append(result, intelDestination{
			Endpoint: endpoint, ActiveNow: activeConnections > 0, ActiveConnections: activeConnections,
			UniqueSources: len(agg.sources), TopSources: topSources, Routes: routeRefs,
			Protocols: sortedSet(agg.protocols), Countries: sortedSet(agg.countries),
			FirstSeen: analyticsTime(agg.metrics.firstSeen), LastSeen: analyticsTime(agg.metrics.lastSeen),
			Connections: agg.metrics.connections, Failed: agg.metrics.failures,
			FailureRate: analyticsFailureRate(agg.metrics),
			TotalBytes:  agg.metrics.bytesSent + agg.metrics.bytesReceived,
			AvgSession:  analyticsAverageSession(agg.metrics), MaxSession: agg.metrics.maxSession,
			Spark: spark, SparkHours: sparkLabels, Signals: signals,
		})
		_ = now
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ActiveNow != result[j].ActiveNow {
			return result[i].ActiveNow
		}
		if result[i].Connections != result[j].Connections {
			return result[i].Connections > result[j].Connections
		}
		return result[i].Endpoint.RawAddress < result[j].Endpoint.RawAddress
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func intelBuildSessions(entries []streamLogEntry, aliases map[string][]string, streams []npm.Stream, descriptions map[int]string, streamsAvailable bool) []intelSession {
	sessions := make([]intelSession, 0, len(entries))
	for _, entry := range entries {
		listener := enrichAnalyticsListener(entry.ProxyAddr, aliases)
		destination := enrichAnalyticsEndpoint(entry.UpstreamAddr, aliases)
		matchedStreams, matchStatus := matchAnalyticsStreams(listener, destination, entry.Protocol, aliases, streams, descriptions, streamsAvailable)
		started := entry.Time
		endedAt := ""
		if !started.IsZero() {
			endedAt = started.Add(time.Duration(entry.SessionTime * float64(time.Second))).UTC().Format(time.RFC3339)
		}
		sessions = append(sessions, intelSession{
			Timestamp: entry.Timestamp, EndedAt: endedAt,
			Source:           enrichAnalyticsEndpoint(entry.ClientIP, aliases),
			ObservedListener: listener, Destination: destination,
			Protocol: strings.ToUpper(entry.Protocol), Country: analyticsCountry(entry.Country),
			RouteID:           intelRouteID(intelRouteKey(entry.ProxyAddr, entry.UpstreamAddr, entry.Protocol)),
			StreamMatchStatus: matchStatus, Streams: matchedStreams,
			Status: entry.Status, Outcome: analyticsOutcome(entry.Status),
			BytesSent: entry.BytesSent, BytesReceived: entry.BytesReceived,
			TotalBytes: entry.BytesSent + entry.BytesReceived, SessionSeconds: entry.SessionTime,
		})
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		left, right := sessions[i], sessions[j]
		leftTime, rightTime := intelSessionTime(left), intelSessionTime(right)
		if !leftTime.Equal(rightTime) {
			return leftTime.After(rightTime)
		}
		return left.Source.IP < right.Source.IP
	})
	return sessions
}

func intelSessionTime(session intelSession) time.Time {
	if value, err := time.Parse(time.RFC3339, session.Timestamp); err == nil {
		return value
	}
	return time.Time{}
}

func intelBuildOverview(entries []streamLogEntry, sessions []intelSession, sourceAggs map[string]*intelSourceAgg, routeAggs map[string]*intelRouteAgg, destAggs map[string]*intelDestinationAgg, globalHourly map[time.Time]*intelHourPoint, denseAxis intelAxis, live intelLiveSummary, liveSources, liveDestinations map[string]int, now time.Time, aliases map[string][]string) *intelOverview {
	overview := &intelOverview{Live: live, ActiveIPs: make([]string, 0), ActiveRoutes: make([]string, 0)}

	activeIPs := make([]string, 0)
	for ip := range liveSources {
		activeIPs = append(activeIPs, ip)
	}
	sort.Strings(activeIPs)
	if len(activeIPs) > 50 {
		activeIPs = activeIPs[:50]
	}
	overview.ActiveIPs = activeIPs

	windowStart := now.Add(-intelNewEntityWindow)
	windowStats := intelWindow{}
	windowSources := make(map[string]struct{})
	windowRoutes := make(map[string]struct{})
	newIPs := make([]intelNewIP, 0)
	for _, agg := range sourceAggs {
		if agg.metrics.firstSeen.IsZero() == false && agg.metrics.firstSeen.After(windowStart) {
			country := ""
			for name := range agg.countries {
				if name != "Unknown" {
					country = name
					break
				}
			}
			newIPs = append(newIPs, intelNewIP{IP: agg.ip, FirstSeen: analyticsTime(agg.metrics.firstSeen), Connections: agg.metrics.connections, Country: country})
		}
	}
	sort.Slice(newIPs, func(i, j int) bool {
		if newIPs[i].Connections != newIPs[j].Connections {
			return newIPs[i].Connections > newIPs[j].Connections
		}
		return newIPs[i].IP < newIPs[j].IP
	})
	if len(newIPs) > 20 {
		newIPs = newIPs[:20]
	}
	overview.NewIPs = newIPs

	for _, entry := range entries {
		if entry.Time.IsZero() || entry.Time.Before(windowStart) || entry.Time.After(now) {
			continue
		}
		windowStats.Connections++
		if entry.Status != http.StatusOK {
			windowStats.Failed++
		}
		windowSources[analyticsSourceIdentity(entry.ClientIP)] = struct{}{}
		windowRoutes[intelRouteID(intelRouteKey(entry.ProxyAddr, entry.UpstreamAddr, entry.Protocol))] = struct{}{}
	}
	windowStats.UniqueSources = len(windowSources)
	windowStats.UniqueRoutes = len(windowRoutes)
	windowStats.NewIPs = len(newIPs)
	overview.Window = windowStats
	overview.TotalConnections = len(entries)

	activeRoutes := make([]string, 0)
	for key, agg := range routeAggs {
		destination := enrichAnalyticsEndpoint(agg.destRaw, aliases)
		if liveDestinations[intelEndpointKey(destination.IP, destination.Port)] > 0 {
			activeRoutes = append(activeRoutes, intelRouteID(key))
		}
	}
	sort.Strings(activeRoutes)
	overview.ActiveRoutes = activeRoutes

	topSources := make([]intelRef, 0, intelMaxTopEntities)
	for _, agg := range sourceAggs {
		topSources = append(topSources, intelRef{ID: agg.ip, Kind: "source", Label: agg.ip, Count: agg.metrics.connections})
	}
	sort.Slice(topSources, func(i, j int) bool {
		if topSources[i].Count != topSources[j].Count {
			return topSources[i].Count > topSources[j].Count
		}
		return topSources[i].ID < topSources[j].ID
	})
	if len(topSources) > intelMaxTopEntities {
		topSources = topSources[:intelMaxTopEntities]
	}
	overview.TopSources = topSources

	topDestinations := make([]intelRef, 0, intelMaxTopEntities)
	for _, agg := range destAggs {
		topDestinations = append(topDestinations, intelRef{ID: agg.raw, Kind: "destination", Label: intelShortLabel(enrichAnalyticsEndpoint(agg.raw, aliases)), Count: agg.metrics.connections})
	}
	sort.Slice(topDestinations, func(i, j int) bool {
		if topDestinations[i].Count != topDestinations[j].Count {
			return topDestinations[i].Count > topDestinations[j].Count
		}
		return topDestinations[i].ID < topDestinations[j].ID
	})
	if len(topDestinations) > intelMaxTopEntities {
		topDestinations = topDestinations[:intelMaxTopEntities]
	}
	overview.TopDestinations = topDestinations

	topRoutes := make([]intelRef, 0, intelMaxTopEntities)
	for key, agg := range routeAggs {
		label := intelShortLabel(enrichAnalyticsListener(agg.listenerRaw, aliases)) + " → " + intelShortLabel(enrichAnalyticsEndpoint(agg.destRaw, aliases))
		topRoutes = append(topRoutes, intelRef{ID: intelRouteID(key), Kind: "route", Label: label, Count: agg.metrics.connections})
	}
	sort.Slice(topRoutes, func(i, j int) bool {
		if topRoutes[i].Count != topRoutes[j].Count {
			return topRoutes[i].Count > topRoutes[j].Count
		}
		return topRoutes[i].ID < topRoutes[j].ID
	})
	if len(topRoutes) > intelMaxTopEntities {
		topRoutes = topRoutes[:intelMaxTopEntities]
	}
	overview.TopRoutes = topRoutes

	longest := append([]intelSession{}, sessions...)
	sort.SliceStable(longest, func(i, j int) bool { return longest[i].SessionSeconds > longest[j].SessionSeconds })
	if len(longest) > intelMaxLongestSessions {
		longest = longest[:intelMaxLongestSessions]
	}
	overview.LongestSessions = longest
	if len(sessions) > intelMaxRecentSessions {
		overview.RecentSessions = sessions[:intelMaxRecentSessions]
	} else {
		overview.RecentSessions = sessions
	}

	overview.Spikes = intelDetectSpikes(globalHourly)
	overview.Hourly = intelHourlySeries(globalHourly, denseAxis)
	return overview
}

func intelDetectSpikes(hourly map[time.Time]*intelHourPoint) []intelSpike {
	hours := make([]time.Time, 0, len(hourly))
	for hour := range hourly {
		hours = append(hours, hour)
	}
	sort.Slice(hours, func(i, j int) bool { return hours[i].Before(hours[j]) })
	spikes := make([]intelSpike, 0)
	if len(hours) < intelSpikeMinBaseline+1 {
		return spikes
	}
	for index := intelSpikeMinBaseline; index < len(hours); index++ {
		baselineStart := index - intelSpikeBaselineBuckets
		if baselineStart < 0 {
			baselineStart = 0
		}
		baseline := hours[baselineStart:index]
		bucket := hourly[hours[index]]
		if bucket == nil || bucket.Connections < intelMinSpikeConnections {
			continue
		}
		values := make([]float64, 0, len(baseline))
		for _, hour := range baseline {
			values = append(values, float64(hourly[hour].Connections))
		}
		mean, deviation := intelMeanStd(values)
		if deviation <= 0 {
			deviation = 1
		}
		z := (float64(bucket.Connections) - mean) / deviation
		if z >= intelSpikeZScore {
			severity := "warning"
			if z >= intelSpikeCriticalZScore {
				severity = "critical"
			}
			spikes = append(spikes, intelSpike{
				Timestamp: bucket.Timestamp, Kind: "connection_spike",
				Connections: bucket.Connections, Failed: bucket.Failed,
				FailureRate: intelRate(bucket.Failed, bucket.Connections),
				Baseline:    mean, ZScore: z, Severity: severity,
			})
			continue
		}
		if bucket.Failed >= intelFailureBurstMin && intelRate(bucket.Failed, bucket.Connections) >= intelFailureBurstRate {
			severity := "warning"
			if intelRate(bucket.Failed, bucket.Connections) >= 0.6 {
				severity = "critical"
			}
			spikes = append(spikes, intelSpike{
				Timestamp: bucket.Timestamp, Kind: "failure_burst",
				Connections: bucket.Connections, Failed: bucket.Failed,
				FailureRate: intelRate(bucket.Failed, bucket.Connections),
				Baseline:    mean, Severity: severity,
			})
		}
	}
	sort.Slice(spikes, func(i, j int) bool {
		if spikes[i].Timestamp != spikes[j].Timestamp {
			return spikes[i].Timestamp > spikes[j].Timestamp
		}
		return spikes[i].ZScore > spikes[j].ZScore
	})
	if len(spikes) > 12 {
		spikes = spikes[:12]
	}
	return spikes
}

func intelRate(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func intelMeanStd(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	var sum float64
	for _, value := range values {
		sum += value
	}
	mean := sum / float64(len(values))
	var varianceSum float64
	for _, value := range values {
		diff := value - mean
		varianceSum += diff * diff
	}
	return mean, math.Sqrt(varianceSum / float64(len(values)))
}

func intelBuildInsights(sourceAggs map[string]*intelSourceAgg, routeAggs map[string]*intelRouteAgg, hourly map[time.Time]*intelHourPoint, sessions []intelSession, now time.Time) []intelInsight {
	insights := make([]intelInsight, 0)

	newIPs := make([]*intelSourceAgg, 0)
	for _, agg := range sourceAggs {
		if agg.metrics.firstSeen.IsZero() == false && now.Sub(agg.metrics.firstSeen) <= intelNewEntityWindow {
			newIPs = append(newIPs, agg)
		}
	}
	sort.Slice(newIPs, func(i, j int) bool {
		if newIPs[i].metrics.connections != newIPs[j].metrics.connections {
			return newIPs[i].metrics.connections > newIPs[j].metrics.connections
		}
		return newIPs[i].ip < newIPs[j].ip
	})
	for index, agg := range newIPs {
		if index >= intelMaxNewIPInsights {
			break
		}
		insights = append(insights, intelInsight{
			ID: "new-ip-" + agg.ip, Severity: "info", Category: "new_ip",
			Title:  "New source IP " + agg.ip,
			Detail: strconv.Itoa(agg.metrics.connections) + " sessions since first seen " + analyticsTime(agg.metrics.firstSeen),
			Link:   "#/ips/" + agg.ip, Timestamp: analyticsTime(agg.metrics.firstSeen),
		})
	}

	spikes := intelDetectSpikes(hourly)
	for _, spike := range spikes {
		title := "Connection spike detected"
		category := "spike"
		detail := strconv.Itoa(spike.Connections) + " connections in the hour of " + spike.Timestamp + " (baseline " + strconv.FormatFloat(spike.Baseline, 'f', 1, 64) + ", z=" + strconv.FormatFloat(spike.ZScore, 'f', 1, 64) + ")"
		if spike.Kind == "failure_burst" {
			title = "Failure burst detected"
			category = "failure_burst"
			detail = strconv.Itoa(spike.Failed) + " failed of " + strconv.Itoa(spike.Connections) + " connections in the hour of " + spike.Timestamp
		}
		insights = append(insights, intelInsight{
			ID: category + "-" + spike.Timestamp, Severity: spike.Severity, Category: category,
			Title: title, Detail: detail, Link: "#/evidence", Timestamp: spike.Timestamp,
		})
	}

	longSessions := make([]intelSession, 0)
	for _, session := range sessions {
		if session.SessionSeconds >= intelLongSessionSeconds {
			longSessions = append(longSessions, session)
		}
	}
	sort.SliceStable(longSessions, func(i, j int) bool { return longSessions[i].SessionSeconds > longSessions[j].SessionSeconds })
	count := 0
	for _, session := range longSessions {
		if count >= 5 {
			break
		}
		count++
		hours := session.SessionSeconds / 3600
		insights = append(insights, intelInsight{
			ID: "long-session-" + session.RouteID + "-" + session.Timestamp, Severity: "warning", Category: "long_session",
			Title:  "Long-lived session (" + strconv.FormatFloat(hours, 'f', 1, 64) + "h)",
			Detail: session.Source.IP + " → " + intelShortLabel(session.Destination) + " via " + intelShortLabel(session.ObservedListener),
			Link:   "#/ips/" + session.Source.IP, Timestamp: session.Timestamp,
		})
	}

	breadthCount := 0
	for _, agg := range sourceAggs {
		if len(agg.destinations) >= intelUnusualSourceDests {
			if breadthCount >= 5 {
				break
			}
			breadthCount++
			insights = append(insights, intelInsight{
				ID: "breadth-" + agg.ip, Severity: "warning", Category: "many_destinations",
				Title:  "Unusual destination breadth for " + agg.ip,
				Detail: strconv.Itoa(len(agg.destinations)) + " distinct destinations reached",
				Link:   "#/ips/" + agg.ip, Timestamp: analyticsTime(agg.metrics.lastSeen),
			})
		}
	}

	failureCount := 0
	for _, agg := range sourceAggs {
		if agg.metrics.connections >= intelUnusualFailureMin && analyticsFailureRate(agg.metrics) >= intelUnusualFailureRate {
			if failureCount >= 5 {
				break
			}
			failureCount++
			insights = append(insights, intelInsight{
				ID: "failures-" + agg.ip, Severity: "warning", Category: "failure_burst",
				Title:  "High failure rate for " + agg.ip,
				Detail: strconv.Itoa(agg.metrics.failures) + " of " + strconv.Itoa(agg.metrics.connections) + " sessions failed",
				Link:   "#/ips/" + agg.ip, Timestamp: analyticsTime(agg.metrics.lastSeen),
			})
		}
	}

	_ = routeAggs
	severityRank := map[string]int{"critical": 0, "warning": 1, "info": 2}
	sort.SliceStable(insights, func(i, j int) bool {
		left, right := insights[i], insights[j]
		if severityRank[left.Severity] != severityRank[right.Severity] {
			return severityRank[left.Severity] < severityRank[right.Severity]
		}
		return left.Timestamp > right.Timestamp
	})
	if len(insights) > intelMaxInsights {
		insights = insights[:intelMaxInsights]
	}
	return insights
}

func intelSourceProfile(aggs map[string]*intelSourceAgg, ip string, aliases map[string][]string, liveSources map[string]int, now time.Time, sparkAxis intelAxis, denseAxis intelAxis) *intelProfile {
	agg := aggs[ip]
	if agg == nil {
		return nil
	}
	profile := &intelProfile{Kind: "source"}
	profile.Source = &intelSource{}
	built := intelBuildSources(map[string]*intelSourceAgg{ip: agg}, aliases, liveSources, now, sparkAxis, 1)
	if len(built) == 1 {
		profile.Source = &built[0]
	}
	profile.Hourly = intelHourlySeries(agg.hourly, denseAxis)
	return profile
}

func intelRouteProfile(aggs map[string]*intelRouteAgg, routeID string, aliases map[string][]string, streams []npm.Stream, descriptions map[int]string, streamsAvailable bool, liveDestinations map[string]int, now time.Time, sparkAxis intelAxis, denseAxis intelAxis) *intelProfile {
	for key, agg := range aggs {
		if intelRouteID(key) != routeID {
			continue
		}
		profile := &intelProfile{Kind: "route"}
		built := intelBuildRoutes(map[string]*intelRouteAgg{key: agg}, aliases, streams, descriptions, streamsAvailable, liveDestinations, now, sparkAxis, 1)
		if len(built) == 1 {
			profile.Route = &built[0]
		}
		profile.Hourly = intelHourlySeries(agg.hourly, denseAxis)
		return profile
	}
	return nil
}

func intelDestinationProfile(aggs map[string]*intelDestinationAgg, raw string, aliases map[string][]string, liveDestinations map[string]int, now time.Time, sparkAxis intelAxis, denseAxis intelAxis) *intelProfile {
	agg := aggs[raw]
	if agg == nil {
		return nil
	}
	profile := &intelProfile{Kind: "destination"}
	built := intelBuildDestinations(map[string]*intelDestinationAgg{raw: agg}, aliases, liveDestinations, now, sparkAxis, 1)
	if len(built) == 1 {
		profile.Destination = &built[0]
	}
	profile.Hourly = intelHourlySeries(agg.hourly, denseAxis)
	return profile
}

func intelSearchEntityMatches(search string, sources []intelSource, routes []intelRoute, destinations []intelDestination) *intelSearchMatches {
	needle := strings.ToLower(search)
	interpreted := "substring"
	if ip := net.ParseIP(search); ip != nil {
		interpreted = "ip"
	}
	matches := &intelSearchMatches{Query: search, Interpreted: interpreted}
	for _, source := range sources {
		if intelSourceMatches(source, needle) {
			matches.Sources = append(matches.Sources, source)
		}
		if len(matches.Sources) >= intelMaxSearchMatches {
			break
		}
	}
	for _, route := range routes {
		if intelRouteMatches(route, needle) {
			matches.Routes = append(matches.Routes, route)
		}
		if len(matches.Routes) >= intelMaxSearchMatches {
			break
		}
	}
	for _, destination := range destinations {
		if intelDestinationMatches(destination, needle) {
			matches.Destinations = append(matches.Destinations, destination)
		}
		if len(matches.Destinations) >= intelMaxSearchMatches {
			break
		}
	}
	return matches
}

func intelSourceMatches(source intelSource, needle string) bool {
	return strings.Contains(strings.ToLower(source.IP), needle) ||
		analyticsContains(strings.Join(source.Aliases, " "), needle) ||
		analyticsContains(strings.Join(source.Protocols, " "), needle) ||
		analyticsContains(strings.Join(source.Countries, " "), needle)
}

func intelRouteMatches(route intelRoute, needle string) bool {
	return analyticsEndpointMatches(route.Listener, needle) || analyticsEndpointMatches(route.Destination, needle) ||
		analyticsContains(route.Protocol, needle) || analyticsContains(strings.Join(route.SourceIPs, " "), needle)
}

func intelDestinationMatches(destination intelDestination, needle string) bool {
	return analyticsEndpointMatches(destination.Endpoint, needle) ||
		analyticsContains(strings.Join(destination.Protocols, " "), needle) ||
		analyticsContains(strings.Join(destination.Countries, " "), needle) ||
		analyticsContains(destination.Endpoint.Port, needle)
}
