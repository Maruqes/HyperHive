package api

import (
	"512SvMan/db"
	"512SvMan/dnsmasq"
	"512SvMan/npm"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oschwald/geoip2-golang"
)

const (
	analyticsDefaultPathLimit    = 100
	analyticsMaxPathLimit        = 500
	analyticsDefaultSessionLimit = 50
	analyticsMaxSessionLimit     = 200
	analyticsMaxDestinations     = 500
	analyticsMaxTimelinePoints   = 744
	analyticsMaxBreakdownItems   = 100
)

type analyticsDependencies struct {
	loadEntries     func() ([]streamLogEntry, *geoip2.Reader, error)
	getAliases      func() ([]dnsmasq.AliasEntry, error)
	listStreams     func(string, string) ([]npm.Stream, error)
	getDescriptions func(context.Context, string, []int) (map[int]string, error)
}

var productionAnalyticsDependencies = analyticsDependencies{
	loadEntries:     loadEntriesWithGeoIP,
	getAliases:      dnsmasq.GetAllAliases,
	listStreams:     npm.ListStreams,
	getDescriptions: db.GetResourceDescriptions,
}

type analyticsEndpoint struct {
	Aliases    []string `json:"aliases"`
	IP         string   `json:"ip"`
	Port       string   `json:"port"`
	RawAddress string   `json:"raw_address"`
	Scope      string   `json:"scope"`
	Display    string   `json:"display"`
}

type analyticsStream struct {
	ID             int      `json:"id"`
	Description    string   `json:"description"`
	IncomingPort   int      `json:"incoming_port"`
	ForwardingHost string   `json:"forwarding_host"`
	ForwardingPort int      `json:"forwarding_port"`
	Protocols      []string `json:"protocols"`
	Enabled        bool     `json:"enabled"`
}

type analyticsPath struct {
	ID                string            `json:"id"`
	Source            analyticsEndpoint `json:"source"`
	ObservedListener  analyticsEndpoint `json:"observed_listener"`
	Destination       analyticsEndpoint `json:"destination"`
	Protocol          string            `json:"protocol"`
	Country           string            `json:"country"`
	StreamMatchStatus string            `json:"stream_match_status"`
	Streams           []analyticsStream `json:"streams"`
	Connections       int               `json:"connections"`
	FailureCount      int               `json:"failure_count"`
	FailureRate       float64           `json:"failure_rate"`
	BytesSent         int64             `json:"bytes_sent"`
	BytesReceived     int64             `json:"bytes_received"`
	TotalBytes        int64             `json:"total_bytes"`
	AvgSession        float64           `json:"avg_session_seconds"`
	MaxSession        float64           `json:"max_session_seconds"`
	FirstSeen         string            `json:"first_seen"`
	LastSeen          string            `json:"last_seen"`
}

type analyticsDestination struct {
	Destination     analyticsEndpoint `json:"destination"`
	Connections     int               `json:"connections"`
	FailureCount    int               `json:"failure_count"`
	FailureRate     float64           `json:"failure_rate"`
	BytesSent       int64             `json:"bytes_sent"`
	BytesReceived   int64             `json:"bytes_received"`
	TotalBytes      int64             `json:"total_bytes"`
	AvgSession      float64           `json:"avg_session_seconds"`
	MaxSession      float64           `json:"max_session_seconds"`
	FirstSeen       string            `json:"first_seen"`
	LastSeen        string            `json:"last_seen"`
	UniqueSources   int               `json:"unique_sources"`
	UniqueListeners int               `json:"unique_listeners"`
	Protocols       []string          `json:"protocols"`
	Countries       []string          `json:"countries"`
}

type analyticsTimelinePoint struct {
	Timestamp          string  `json:"timestamp"`
	Connections        int     `json:"connections"`
	FailureCount       int     `json:"failure_count"`
	FailureRate        float64 `json:"failure_rate"`
	BytesSent          int64   `json:"bytes_sent"`
	BytesReceived      int64   `json:"bytes_received"`
	TotalBytes         int64   `json:"total_bytes"`
	AvgSession         float64 `json:"avg_session_seconds"`
	UniqueSources      int     `json:"unique_sources"`
	UniqueDestinations int     `json:"unique_destinations"`
}

type analyticsSession struct {
	Timestamp         string            `json:"timestamp"`
	Source            analyticsEndpoint `json:"source"`
	ObservedListener  analyticsEndpoint `json:"observed_listener"`
	Destination       analyticsEndpoint `json:"destination"`
	Protocol          string            `json:"protocol"`
	Country           string            `json:"country"`
	StreamMatchStatus string            `json:"stream_match_status"`
	Streams           []analyticsStream `json:"streams"`
	Status            int               `json:"status"`
	Outcome           string            `json:"outcome"`
	BytesSent         int64             `json:"bytes_sent"`
	BytesReceived     int64             `json:"bytes_received"`
	TotalBytes        int64             `json:"total_bytes"`
	SessionSeconds    float64           `json:"session_seconds"`
}

type analyticsBreakdownItem struct {
	Value        string `json:"value"`
	Connections  int    `json:"connections"`
	FailureCount int    `json:"failure_count"`
	TotalBytes   int64  `json:"total_bytes"`
}

type analyticsBreakdowns struct {
	Protocols         []analyticsBreakdownItem `json:"protocols"`
	Countries         []analyticsBreakdownItem `json:"countries"`
	Ports             []analyticsBreakdownItem `json:"ports"`
	Outcomes          []analyticsBreakdownItem `json:"outcomes"`
	SourceScopes      []analyticsBreakdownItem `json:"source_scopes"`
	DestinationScopes []analyticsBreakdownItem `json:"destination_scopes"`
}

type analyticsDateRange struct {
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
}

type analyticsSummary struct {
	TotalConnections      int                `json:"total_connections"`
	UniqueSources         int                `json:"unique_sources"`
	UniqueListeners       int                `json:"unique_listeners"`
	UniqueDestinations    int                `json:"unique_destinations"`
	UniqueCountries       int                `json:"unique_countries"`
	SuccessfulConnections int                `json:"successful_connections"`
	FailedConnections     int                `json:"failed_connections"`
	BytesSent             int64              `json:"bytes_sent"`
	BytesReceived         int64              `json:"bytes_received"`
	TotalBytes            int64              `json:"total_bytes"`
	AvgSession            float64            `json:"avg_session_seconds"`
	MaxSession            float64            `json:"max_session_seconds"`
	DateRange             analyticsDateRange `json:"date_range"`
}

type analyticsAvailability struct {
	GeoIP        bool `json:"geoip"`
	DNSAliases   bool `json:"dns_aliases"`
	NPMStreams   bool `json:"npm_streams"`
	Descriptions bool `json:"descriptions"`
}

type analyticsLimits struct {
	PathLimit                      int             `json:"path_limit"`
	SessionLimit                   int             `json:"session_limit"`
	DestinationLimit               int             `json:"destination_limit"`
	TimelineLimit                  int             `json:"timeline_limit"`
	BreakdownLimit                 int             `json:"breakdown_limit"`
	PathsAvailable                 int             `json:"paths_available"`
	SessionsAvailable              int             `json:"sessions_available"`
	DestinationsAvailable          int             `json:"destinations_available"`
	TimelinePointsAvailable        int             `json:"timeline_points_available"`
	BreakdownItemsAvailable        int             `json:"breakdown_items_available"`
	BreakdownItemsReturned         int             `json:"breakdown_items_returned"`
	BreakdownsAvailable            map[string]int  `json:"breakdowns_available"`
	BreakdownsReturned             map[string]int  `json:"breakdowns_returned"`
	BreakdownsTruncatedByDimension map[string]bool `json:"breakdowns_truncated_by_dimension"`
	PathsTruncated                 bool            `json:"paths_truncated"`
	SessionsTruncated              bool            `json:"sessions_truncated"`
	DestinationsTruncated          bool            `json:"destinations_truncated"`
	TimelineTruncated              bool            `json:"timeline_truncated"`
	BreakdownsTruncated            bool            `json:"breakdowns_truncated"`
}

type analyticsMetadata struct {
	GeneratedAt           string                `json:"generated_at"`
	TotalAvailableEntries int                   `json:"total_available_entries"`
	FilteredEntries       int                   `json:"filtered_entries"`
	AliasesCount          int                   `json:"aliases_count"`
	Availability          analyticsAvailability `json:"availability"`
	Warnings              []string              `json:"warnings"`
	Limits                analyticsLimits       `json:"limits"`
}

type analyticsResponse struct {
	PathSemantics  string                   `json:"path_semantics"`
	Metadata       analyticsMetadata        `json:"metadata"`
	Summary        analyticsSummary         `json:"summary"`
	Paths          []analyticsPath          `json:"paths"`
	Destinations   []analyticsDestination   `json:"destinations"`
	HourlyTimeline []analyticsTimelinePoint `json:"hourly_timeline"`
	RecentSessions []analyticsSession       `json:"recent_sessions"`
	Breakdowns     analyticsBreakdowns      `json:"breakdowns"`
}

type analyticsFilters struct {
	start        time.Time
	endExclusive time.Time
	source       string
	listener     string
	destination  string
	protocol     string
	country      string
	outcome      string
	pathLimit    int
	sessionLimit int
}

type analyticsEndpointParts struct {
	raw  string
	host string
	ip   string
	port string
}

type analyticsAccumulator struct {
	connections   int
	failures      int
	bytesSent     int64
	bytesReceived int64
	sessionSum    float64
	maxSession    float64
	firstSeen     time.Time
	lastSeen      time.Time
}

type analyticsPathAccumulator struct {
	key         string
	source      string
	listener    string
	destination string
	protocol    string
	country     string
	metrics     analyticsAccumulator
}

type analyticsDestinationAccumulator struct {
	raw       string
	metrics   analyticsAccumulator
	sources   map[string]struct{}
	listeners map[string]struct{}
	protocols map[string]struct{}
	countries map[string]struct{}
}

type analyticsTimelineAccumulator struct {
	metrics      analyticsAccumulator
	sources      map[string]struct{}
	destinations map[string]struct{}
}

func getStreamAnalytics(w http.ResponseWriter, r *http.Request) {
	serveStreamAnalytics(w, r, productionAnalyticsDependencies)
}

func serveStreamAnalytics(w http.ResponseWriter, r *http.Request, deps analyticsDependencies) {
	filters, err := parseAnalyticsFilters(r)
	if err != nil {
		respondJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	entries, geoIPDB, err := deps.loadEntries()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			respondJSONError(w, http.StatusNotFound, "stream-proxy.log not found")
			return
		}
		respondJSONError(w, http.StatusInternalServerError, "failed to load stream telemetry")
		return
	}

	metadata := analyticsMetadata{
		GeneratedAt:           time.Now().UTC().Format(time.RFC3339),
		TotalAvailableEntries: len(entries),
		Availability:          analyticsAvailability{GeoIP: geoIPDB != nil},
		Warnings: []string{
			"DNS aliases and NPM stream correlations use current configuration and may not describe historical state",
		},
	}
	if geoIPDB == nil {
		metadata.Warnings = append(metadata.Warnings, "GeoIP data is unavailable; countries may be Unknown")
	}

	aliasEntries, aliasErr := deps.getAliases()
	aliasMap := buildAnalyticsAliasMap(aliasEntries)
	metadata.AliasesCount = analyticsAliasCount(aliasMap)
	metadata.Availability.DNSAliases = aliasErr == nil
	if aliasErr != nil {
		metadata.Warnings = append(metadata.Warnings, "managed DNS aliases are unavailable")
	}

	entries = filterAnalyticsEntries(entries, filters, aliasMap)
	metadata.FilteredEntries = len(entries)

	streams, streamErr := deps.listStreams(baseURL, GetTokenFromContext(r))
	metadata.Availability.NPMStreams = streamErr == nil
	descriptions := make(map[int]string)
	if streamErr != nil {
		streams = []npm.Stream{}
		metadata.Warnings = append(metadata.Warnings, "NPM stream definitions are unavailable")
	} else {
		ids := make([]int, len(streams))
		for i := range streams {
			ids[i] = streams[i].ID
		}
		var descriptionErr error
		descriptions, descriptionErr = deps.getDescriptions(r.Context(), "npm_stream", ids)
		metadata.Availability.Descriptions = descriptionErr == nil
		if descriptionErr != nil {
			descriptions = make(map[int]string)
			metadata.Warnings = append(metadata.Warnings, "NPM stream descriptions are unavailable")
		}
	}

	response := aggregateStreamAnalytics(entries, aliasMap, streams, descriptions, metadata.Availability.NPMStreams, filters)
	metadata.Limits = response.Metadata.Limits
	response.Metadata = metadata

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func parseAnalyticsFilters(r *http.Request) (analyticsFilters, error) {
	query := r.URL.Query()
	filters := analyticsFilters{
		source:       strings.TrimSpace(query.Get("source")),
		listener:     strings.TrimSpace(query.Get("listener")),
		destination:  strings.TrimSpace(query.Get("destination")),
		protocol:     strings.TrimSpace(query.Get("protocol")),
		country:      strings.TrimSpace(query.Get("country")),
		outcome:      strings.ToLower(strings.TrimSpace(query.Get("outcome"))),
		pathLimit:    analyticsDefaultPathLimit,
		sessionLimit: analyticsDefaultSessionLimit,
	}

	const dateLayout = "2006-01-02"
	if value := query.Get("start"); value != "" {
		parsed, err := time.Parse(dateLayout, value)
		if err != nil {
			return analyticsFilters{}, errors.New("start must use YYYY-MM-DD")
		}
		filters.start = parsed
	}
	if value := query.Get("end"); value != "" {
		parsed, err := time.Parse(dateLayout, value)
		if err != nil {
			return analyticsFilters{}, errors.New("end must use YYYY-MM-DD")
		}
		if !filters.start.IsZero() && filters.start.After(parsed) {
			return analyticsFilters{}, errors.New("start must not be after end")
		}
		filters.endExclusive = parsed.AddDate(0, 0, 1)
	}
	if filters.outcome != "" && filters.outcome != "success" && filters.outcome != "failed" {
		return analyticsFilters{}, errors.New("outcome must be success or failed")
	}

	var err error
	filters.pathLimit, err = parseAnalyticsLimit(query.Get("path_limit"), analyticsDefaultPathLimit, analyticsMaxPathLimit, "path_limit")
	if err != nil {
		return analyticsFilters{}, err
	}
	filters.sessionLimit, err = parseAnalyticsLimit(query.Get("session_limit"), analyticsDefaultSessionLimit, analyticsMaxSessionLimit, "session_limit")
	if err != nil {
		return analyticsFilters{}, err
	}
	return filters, nil
}

func parseAnalyticsLimit(value string, defaultValue, maxValue int, name string) (int, error) {
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, errors.New(name + " must be a positive integer")
	}
	if parsed > maxValue {
		return maxValue, nil
	}
	return parsed, nil
}

func parseAnalyticsEndpoint(raw string) analyticsEndpointParts {
	raw = strings.TrimSpace(raw)
	parts := analyticsEndpointParts{raw: raw, host: raw}
	if raw == "" {
		return parts
	}

	bare := strings.TrimPrefix(strings.TrimSuffix(raw, "]"), "[")
	if ip := net.ParseIP(bare); ip != nil {
		parts.host = bare
		parts.ip = ip.String()
		return parts
	}

	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return parts
	}
	parts.host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	parts.port = port
	if ip := net.ParseIP(parts.host); ip != nil {
		parts.ip = ip.String()
	}
	return parts
}

func parseAnalyticsListener(raw string) analyticsEndpointParts {
	parts := parseAnalyticsEndpoint(raw)
	if parts.port != "" {
		if !analyticsValidPort(parts.port) {
			parts.port = ""
		}
		return parts
	}

	raw = strings.TrimSpace(raw)
	lastColon := strings.LastIndexByte(raw, ':')
	if lastColon <= 0 || lastColon == len(raw)-1 {
		return parts
	}
	host := raw[:lastColon]
	port := raw[lastColon+1:]
	if !analyticsValidPort(port) {
		return parts
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() != nil {
		return parts
	}
	parts.host = host
	parts.ip = ip.String()
	parts.port = port
	return parts
}

func analyticsValidPort(port string) bool {
	if port == "" {
		return false
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	parsed, err := strconv.Atoi(port)
	return err == nil && parsed >= 1 && parsed <= 65535
}

func buildAnalyticsAliasMap(entries []dnsmasq.AliasEntry) map[string][]string {
	sets := make(map[string]map[string]struct{})
	for _, entry := range entries {
		ip := net.ParseIP(strings.TrimSpace(entry.IP))
		alias := strings.TrimSpace(entry.Alias)
		if ip == nil || alias == "" {
			continue
		}
		key := ip.String()
		if sets[key] == nil {
			sets[key] = make(map[string]struct{})
		}
		sets[key][alias] = struct{}{}
	}

	result := make(map[string][]string, len(sets))
	for ip, aliases := range sets {
		result[ip] = sortedSet(aliases)
	}
	return result
}

func analyticsAliasCount(aliases map[string][]string) int {
	count := 0
	for _, names := range aliases {
		count += len(names)
	}
	return count
}

func enrichAnalyticsEndpoint(raw string, aliases map[string][]string) analyticsEndpoint {
	return enrichAnalyticsEndpointParts(parseAnalyticsEndpoint(raw), aliases)
}

func enrichAnalyticsListener(raw string, aliases map[string][]string) analyticsEndpoint {
	return enrichAnalyticsEndpointParts(parseAnalyticsListener(raw), aliases)
}

func enrichAnalyticsEndpointParts(parts analyticsEndpointParts, aliases map[string][]string) analyticsEndpoint {
	endpointAliases := append([]string{}, aliases[parts.ip]...)
	displayBase := parts.ip
	if displayBase == "" {
		displayBase = parts.host
	}
	if len(endpointAliases) > 0 {
		displayBase = strings.Join(endpointAliases, ", ") + " (" + parts.ip + ")"
	}
	display := displayBase
	if parts.port != "" {
		if len(endpointAliases) == 0 && strings.Contains(displayBase, ":") {
			display = "[" + displayBase + "]:" + parts.port
		} else {
			display += ":" + parts.port
		}
	}
	return analyticsEndpoint{
		Aliases:    endpointAliases,
		IP:         parts.ip,
		Port:       parts.port,
		RawAddress: parts.raw,
		Scope:      analyticsIPScope(parts.ip),
		Display:    display,
	}
}

func analyticsIPScope(value string) string {
	ip := net.ParseIP(value)
	if ip == nil || ip.IsUnspecified() {
		return "unknown"
	}
	if ip.IsLoopback() {
		return "loopback"
	}
	if ip.IsMulticast() {
		return "multicast"
	}
	if ip.IsLinkLocalUnicast() {
		return "link-local"
	}
	if ip.IsPrivate() {
		return "private"
	}
	if ip.IsGlobalUnicast() {
		return "public"
	}
	return "unknown"
}

func filterAnalyticsEntries(entries []streamLogEntry, filters analyticsFilters, aliases map[string][]string) []streamLogEntry {
	filtered := make([]streamLogEntry, 0, len(entries))
	for _, entry := range entries {
		if (!filters.start.IsZero() || !filters.endExclusive.IsZero()) && entry.Time.IsZero() {
			continue
		}
		if !filters.start.IsZero() && entry.Time.Before(filters.start) {
			continue
		}
		if !filters.endExclusive.IsZero() && !entry.Time.Before(filters.endExclusive) {
			continue
		}
		if filters.source != "" && !analyticsEndpointMatches(enrichAnalyticsEndpoint(entry.ClientIP, aliases), filters.source) {
			continue
		}
		if filters.listener != "" && !analyticsEndpointMatches(enrichAnalyticsListener(entry.ProxyAddr, aliases), filters.listener) {
			continue
		}
		if filters.destination != "" && !analyticsEndpointMatches(enrichAnalyticsEndpoint(entry.UpstreamAddr, aliases), filters.destination) {
			continue
		}
		if filters.protocol != "" && !analyticsContains(entry.Protocol, filters.protocol) {
			continue
		}
		if filters.country != "" && !analyticsContains(analyticsCountry(entry.Country), filters.country) {
			continue
		}
		outcome := analyticsOutcome(entry.Status)
		if filters.outcome != "" && outcome != filters.outcome {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func analyticsEndpointMatches(endpoint analyticsEndpoint, value string) bool {
	candidates := []string{endpoint.RawAddress, endpoint.IP, endpoint.Display, endpoint.Port}
	candidates = append(candidates, endpoint.Aliases...)
	for _, candidate := range candidates {
		if analyticsContains(candidate, value) {
			return true
		}
	}
	return false
}

func analyticsContains(value, search string) bool {
	search = strings.ToLower(strings.TrimSpace(search))
	if search == "" {
		return true
	}
	return strings.Contains(strings.ToLower(value), search)
}

func aggregateStreamAnalytics(entries []streamLogEntry, aliases map[string][]string, streams []npm.Stream, descriptions map[int]string, streamsAvailable bool, filters analyticsFilters) analyticsResponse {
	response := analyticsResponse{
		PathSemantics:  "Paths represent only observed log fields: source -> observed proxy/listener -> logged upstream destination. No unobserved network hops or interfaces are claimed. DNS aliases and stream correlations use current configuration and may not describe historical state.",
		Paths:          make([]analyticsPath, 0),
		Destinations:   make([]analyticsDestination, 0),
		HourlyTimeline: make([]analyticsTimelinePoint, 0),
		RecentSessions: make([]analyticsSession, 0),
		Breakdowns: analyticsBreakdowns{
			Protocols:         make([]analyticsBreakdownItem, 0),
			Countries:         make([]analyticsBreakdownItem, 0),
			Ports:             make([]analyticsBreakdownItem, 0),
			Outcomes:          make([]analyticsBreakdownItem, 0),
			SourceScopes:      make([]analyticsBreakdownItem, 0),
			DestinationScopes: make([]analyticsBreakdownItem, 0),
		},
	}

	pathMap := make(map[string]*analyticsPathAccumulator)
	destinationMap := make(map[string]*analyticsDestinationAccumulator)
	timelineMap := make(map[time.Time]*analyticsTimelineAccumulator)
	sources := make(map[string]struct{})
	listeners := make(map[string]struct{})
	destinations := make(map[string]struct{})
	countries := make(map[string]struct{})
	breakdownMaps := []map[string]*analyticsBreakdownItem{
		make(map[string]*analyticsBreakdownItem), make(map[string]*analyticsBreakdownItem),
		make(map[string]*analyticsBreakdownItem), make(map[string]*analyticsBreakdownItem),
		make(map[string]*analyticsBreakdownItem), make(map[string]*analyticsBreakdownItem),
	}

	for _, entry := range entries {
		country := analyticsCountry(entry.Country)
		protocol := strings.ToUpper(entry.Protocol)
		pathKey := strings.Join([]string{entry.ClientIP, entry.ProxyAddr, entry.UpstreamAddr, protocol, country}, "\x00")
		path := pathMap[pathKey]
		if path == nil {
			path = &analyticsPathAccumulator{key: pathKey, source: entry.ClientIP, listener: entry.ProxyAddr, destination: entry.UpstreamAddr, protocol: protocol, country: country}
			pathMap[pathKey] = path
		}
		addAnalyticsMetrics(&path.metrics, entry)

		destination := destinationMap[entry.UpstreamAddr]
		if destination == nil {
			destination = &analyticsDestinationAccumulator{
				raw: entry.UpstreamAddr, sources: make(map[string]struct{}), listeners: make(map[string]struct{}),
				protocols: make(map[string]struct{}), countries: make(map[string]struct{}),
			}
			destinationMap[entry.UpstreamAddr] = destination
		}
		addAnalyticsMetrics(&destination.metrics, entry)
		destination.sources[entry.ClientIP] = struct{}{}
		destination.listeners[entry.ProxyAddr] = struct{}{}
		destination.protocols[protocol] = struct{}{}
		destination.countries[country] = struct{}{}

		if !entry.Time.IsZero() {
			hour := entry.Time.UTC().Truncate(time.Hour)
			if timelineMap[hour] == nil {
				timelineMap[hour] = &analyticsTimelineAccumulator{
					sources:      make(map[string]struct{}),
					destinations: make(map[string]struct{}),
				}
			}
			bucket := timelineMap[hour]
			addAnalyticsMetrics(&bucket.metrics, entry)
			bucket.sources[entry.ClientIP] = struct{}{}
			bucket.destinations[entry.UpstreamAddr] = struct{}{}
		}

		sourceEndpoint := enrichAnalyticsEndpoint(entry.ClientIP, aliases)
		destinationEndpoint := enrichAnalyticsEndpoint(entry.UpstreamAddr, aliases)
		values := []string{protocol, country, analyticsPort(entry.UpstreamAddr), analyticsOutcome(entry.Status), sourceEndpoint.Scope, destinationEndpoint.Scope}
		for i, value := range values {
			item := breakdownMaps[i][value]
			if item == nil {
				item = &analyticsBreakdownItem{Value: value}
				breakdownMaps[i][value] = item
			}
			item.Connections++
			if entry.Status != http.StatusOK {
				item.FailureCount++
			}
			item.TotalBytes += entry.BytesSent + entry.BytesReceived
		}

		sources[entry.ClientIP] = struct{}{}
		listeners[entry.ProxyAddr] = struct{}{}
		destinations[entry.UpstreamAddr] = struct{}{}
		if country != "Unknown" {
			countries[country] = struct{}{}
		}
		addAnalyticsSummary(&response.Summary, entry)
	}

	response.Summary.UniqueSources = len(sources)
	response.Summary.UniqueListeners = len(listeners)
	response.Summary.UniqueDestinations = len(destinations)
	response.Summary.UniqueCountries = len(countries)
	if response.Summary.TotalConnections > 0 {
		response.Summary.AvgSession /= float64(response.Summary.TotalConnections)
	}

	pathAccumulators := make([]*analyticsPathAccumulator, 0, len(pathMap))
	for _, accumulator := range pathMap {
		pathAccumulators = append(pathAccumulators, accumulator)
	}
	sort.Slice(pathAccumulators, func(i, j int) bool {
		left := pathAccumulators[i]
		right := pathAccumulators[j]
		leftBytes := left.metrics.bytesSent + left.metrics.bytesReceived
		rightBytes := right.metrics.bytesSent + right.metrics.bytesReceived
		if leftBytes != rightBytes {
			return leftBytes > rightBytes
		}
		if left.metrics.connections != right.metrics.connections {
			return left.metrics.connections > right.metrics.connections
		}
		return left.key < right.key
	})
	pathsAvailable := len(pathAccumulators)
	if len(pathAccumulators) > filters.pathLimit {
		pathAccumulators = pathAccumulators[:filters.pathLimit]
	}
	for _, accumulator := range pathAccumulators {
		source := enrichAnalyticsEndpoint(accumulator.source, aliases)
		listener := enrichAnalyticsListener(accumulator.listener, aliases)
		destination := enrichAnalyticsEndpoint(accumulator.destination, aliases)
		matchedStreams, matchStatus := matchAnalyticsStreams(listener, destination, accumulator.protocol, aliases, streams, descriptions, streamsAvailable)
		response.Paths = append(response.Paths, analyticsPath{
			ID: analyticsPathID(accumulator.key), Source: source, ObservedListener: listener, Destination: destination,
			Protocol: accumulator.protocol, Country: accumulator.country, StreamMatchStatus: matchStatus, Streams: matchedStreams,
			Connections: accumulator.metrics.connections, FailureCount: accumulator.metrics.failures,
			FailureRate: analyticsFailureRate(accumulator.metrics), BytesSent: accumulator.metrics.bytesSent,
			BytesReceived: accumulator.metrics.bytesReceived, TotalBytes: accumulator.metrics.bytesSent + accumulator.metrics.bytesReceived,
			AvgSession: analyticsAverageSession(accumulator.metrics), MaxSession: accumulator.metrics.maxSession,
			FirstSeen: analyticsTime(accumulator.metrics.firstSeen), LastSeen: analyticsTime(accumulator.metrics.lastSeen),
		})
	}

	for _, accumulator := range destinationMap {
		response.Destinations = append(response.Destinations, analyticsDestination{
			Destination: enrichAnalyticsEndpoint(accumulator.raw, aliases), Connections: accumulator.metrics.connections,
			FailureCount: accumulator.metrics.failures, FailureRate: analyticsFailureRate(accumulator.metrics),
			BytesSent: accumulator.metrics.bytesSent, BytesReceived: accumulator.metrics.bytesReceived,
			TotalBytes: accumulator.metrics.bytesSent + accumulator.metrics.bytesReceived,
			AvgSession: analyticsAverageSession(accumulator.metrics), MaxSession: accumulator.metrics.maxSession,
			FirstSeen: analyticsTime(accumulator.metrics.firstSeen), LastSeen: analyticsTime(accumulator.metrics.lastSeen),
			UniqueSources: len(accumulator.sources), UniqueListeners: len(accumulator.listeners),
			Protocols: sortedSet(accumulator.protocols), Countries: sortedSet(accumulator.countries),
		})
	}
	sort.Slice(response.Destinations, func(i, j int) bool {
		if response.Destinations[i].TotalBytes != response.Destinations[j].TotalBytes {
			return response.Destinations[i].TotalBytes > response.Destinations[j].TotalBytes
		}
		if response.Destinations[i].Connections != response.Destinations[j].Connections {
			return response.Destinations[i].Connections > response.Destinations[j].Connections
		}
		return response.Destinations[i].Destination.RawAddress < response.Destinations[j].Destination.RawAddress
	})
	cappedDestinations, destinationsAvailable := capAnalyticsDestinations(response.Destinations, analyticsMaxDestinations)
	response.Destinations = cappedDestinations

	hours := make([]time.Time, 0, len(timelineMap))
	for hour := range timelineMap {
		hours = append(hours, hour)
	}
	sort.Slice(hours, func(i, j int) bool { return hours[i].Before(hours[j]) })
	for _, hour := range hours {
		bucket := timelineMap[hour]
		metrics := bucket.metrics
		response.HourlyTimeline = append(response.HourlyTimeline, analyticsTimelinePoint{
			Timestamp: hour.Format(time.RFC3339), Connections: metrics.connections, FailureCount: metrics.failures,
			FailureRate: analyticsFailureRate(metrics), BytesSent: metrics.bytesSent, BytesReceived: metrics.bytesReceived,
			TotalBytes: metrics.bytesSent + metrics.bytesReceived, AvgSession: analyticsAverageSession(metrics),
			UniqueSources: len(bucket.sources), UniqueDestinations: len(bucket.destinations),
		})
	}
	cappedTimeline, timelinePointsAvailable := capLatestAnalyticsTimeline(response.HourlyTimeline, analyticsMaxTimelinePoints)
	response.HourlyTimeline = cappedTimeline

	recent := append([]streamLogEntry{}, entries...)
	sort.SliceStable(recent, func(i, j int) bool {
		if !recent[i].Time.Equal(recent[j].Time) {
			return recent[i].Time.After(recent[j].Time)
		}
		return analyticsSessionKey(recent[i]) < analyticsSessionKey(recent[j])
	})
	sessionsAvailable := len(recent)
	if len(recent) > filters.sessionLimit {
		recent = recent[:filters.sessionLimit]
	}
	for _, entry := range recent {
		listener := enrichAnalyticsListener(entry.ProxyAddr, aliases)
		destination := enrichAnalyticsEndpoint(entry.UpstreamAddr, aliases)
		matchedStreams, matchStatus := matchAnalyticsStreams(listener, destination, entry.Protocol, aliases, streams, descriptions, streamsAvailable)
		response.RecentSessions = append(response.RecentSessions, analyticsSession{
			Timestamp: entry.Timestamp, Source: enrichAnalyticsEndpoint(entry.ClientIP, aliases),
			ObservedListener: listener, Destination: destination,
			Protocol: strings.ToUpper(entry.Protocol), Country: analyticsCountry(entry.Country),
			StreamMatchStatus: matchStatus, Streams: matchedStreams, Status: entry.Status,
			Outcome: analyticsOutcome(entry.Status), BytesSent: entry.BytesSent, BytesReceived: entry.BytesReceived,
			TotalBytes: entry.BytesSent + entry.BytesReceived, SessionSeconds: entry.SessionTime,
		})
	}

	breakdownItemsAvailable := 0
	breakdownItemsReturned := 0
	breakdownsTruncated := false
	breakdownsAvailable := make(map[string]int, len(breakdownMaps))
	breakdownsReturned := make(map[string]int, len(breakdownMaps))
	breakdownsTruncatedByDimension := make(map[string]bool, len(breakdownMaps))
	breakdownNames := []string{"protocols", "countries", "ports", "outcomes", "source_scopes", "destination_scopes"}
	breakdownTargets := []*[]analyticsBreakdownItem{
		&response.Breakdowns.Protocols,
		&response.Breakdowns.Countries,
		&response.Breakdowns.Ports,
		&response.Breakdowns.Outcomes,
		&response.Breakdowns.SourceScopes,
		&response.Breakdowns.DestinationScopes,
	}
	for i, target := range breakdownTargets {
		items, available := analyticsBreakdownSlice(breakdownMaps[i], analyticsMaxBreakdownItems)
		*target = items
		breakdownItemsAvailable += available
		breakdownItemsReturned += len(items)
		truncated := analyticsCollectionTruncated(available, len(items))
		breakdownsAvailable[breakdownNames[i]] = available
		breakdownsReturned[breakdownNames[i]] = len(items)
		breakdownsTruncatedByDimension[breakdownNames[i]] = truncated
		breakdownsTruncated = breakdownsTruncated || truncated
	}
	response.Metadata.Limits = analyticsLimits{
		PathLimit: filters.pathLimit, SessionLimit: filters.sessionLimit,
		DestinationLimit: analyticsMaxDestinations, TimelineLimit: analyticsMaxTimelinePoints,
		BreakdownLimit: analyticsMaxBreakdownItems, PathsAvailable: pathsAvailable,
		SessionsAvailable: sessionsAvailable, DestinationsAvailable: destinationsAvailable,
		TimelinePointsAvailable: timelinePointsAvailable, BreakdownItemsAvailable: breakdownItemsAvailable,
		BreakdownItemsReturned: breakdownItemsReturned, BreakdownsAvailable: breakdownsAvailable,
		BreakdownsReturned: breakdownsReturned, BreakdownsTruncatedByDimension: breakdownsTruncatedByDimension,
		PathsTruncated:        pathsAvailable > filters.pathLimit,
		SessionsTruncated:     sessionsAvailable > filters.sessionLimit,
		DestinationsTruncated: analyticsCollectionTruncated(destinationsAvailable, len(response.Destinations)),
		TimelineTruncated:     analyticsCollectionTruncated(timelinePointsAvailable, len(response.HourlyTimeline)),
		BreakdownsTruncated:   breakdownsTruncated,
	}
	return response
}

func addAnalyticsMetrics(metrics *analyticsAccumulator, entry streamLogEntry) {
	metrics.connections++
	if entry.Status != http.StatusOK {
		metrics.failures++
	}
	metrics.bytesSent += entry.BytesSent
	metrics.bytesReceived += entry.BytesReceived
	metrics.sessionSum += entry.SessionTime
	if entry.SessionTime > metrics.maxSession {
		metrics.maxSession = entry.SessionTime
	}
	if !entry.Time.IsZero() {
		if metrics.firstSeen.IsZero() || entry.Time.Before(metrics.firstSeen) {
			metrics.firstSeen = entry.Time
		}
		if metrics.lastSeen.IsZero() || entry.Time.After(metrics.lastSeen) {
			metrics.lastSeen = entry.Time
		}
	}
}

func addAnalyticsSummary(summary *analyticsSummary, entry streamLogEntry) {
	summary.TotalConnections++
	if entry.Status == http.StatusOK {
		summary.SuccessfulConnections++
	} else {
		summary.FailedConnections++
	}
	summary.BytesSent += entry.BytesSent
	summary.BytesReceived += entry.BytesReceived
	summary.TotalBytes += entry.BytesSent + entry.BytesReceived
	summary.AvgSession += entry.SessionTime
	if entry.SessionTime > summary.MaxSession {
		summary.MaxSession = entry.SessionTime
	}
	if !entry.Time.IsZero() {
		first, _ := time.Parse(time.RFC3339, summary.DateRange.FirstSeen)
		last, _ := time.Parse(time.RFC3339, summary.DateRange.LastSeen)
		if summary.DateRange.FirstSeen == "" || entry.Time.Before(first) {
			summary.DateRange.FirstSeen = analyticsTime(entry.Time)
		}
		if summary.DateRange.LastSeen == "" || entry.Time.After(last) {
			summary.DateRange.LastSeen = analyticsTime(entry.Time)
		}
	}
}

func matchAnalyticsStreams(listener, destination analyticsEndpoint, protocol string, aliases map[string][]string, streams []npm.Stream, descriptions map[int]string, available bool) ([]analyticsStream, string) {
	matches := make([]analyticsStream, 0)
	if !available {
		return matches, "unavailable"
	}
	listenerPort, listenerErr := strconv.Atoi(listener.Port)
	destinationPort, destinationErr := strconv.Atoi(destination.Port)
	if listenerErr != nil || destinationErr != nil {
		return matches, "unmatched"
	}
	indeterminate := make([]analyticsStream, 0)
	for _, stream := range streams {
		if stream.Incoming_port != listenerPort || stream.Forwarding_port != destinationPort {
			continue
		}

		hostEvidence := analyticsStreamHostEvidence(stream.Forwarding_host, destination, aliases)
		if hostEvidence == analyticsEvidenceMismatch {
			continue
		}
		protocolKnown := analyticsStreamProtocolKnown(protocol)
		if protocolKnown && !analyticsStreamProtocolMatches(stream, protocol) {
			continue
		}
		if !protocolKnown && !stream.Tcp_forwarding && !stream.Udp_forwarding {
			continue
		}

		streamInfo := buildAnalyticsStream(stream, descriptions[stream.ID])
		if !protocolKnown || hostEvidence == analyticsEvidenceIndeterminate {
			indeterminate = append(indeterminate, streamInfo)
			continue
		}
		matches = append(matches, streamInfo)
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })
	sort.Slice(indeterminate, func(i, j int) bool { return indeterminate[i].ID < indeterminate[j].ID })
	if len(matches) > 0 && len(indeterminate) > 0 {
		candidates := append(matches, indeterminate...)
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
		return candidates, "ambiguous"
	}
	if len(matches) == 1 {
		return matches, "matched"
	}
	if len(matches) > 1 {
		return matches, "ambiguous"
	}
	if len(indeterminate) > 0 {
		return indeterminate, "indeterminate"
	}
	return matches, "unmatched"
}

func buildAnalyticsStream(stream npm.Stream, description string) analyticsStream {
	protocols := make([]string, 0, 2)
	if stream.Tcp_forwarding {
		protocols = append(protocols, "TCP")
	}
	if stream.Udp_forwarding {
		protocols = append(protocols, "UDP")
	}
	return analyticsStream{
		ID: stream.ID, Description: description, IncomingPort: stream.Incoming_port,
		ForwardingHost: stream.Forwarding_host, ForwardingPort: stream.Forwarding_port,
		Protocols: protocols, Enabled: stream.Enabled,
	}
}

func analyticsStreamProtocolKnown(protocol string) bool {
	protocol = strings.ToUpper(strings.TrimSpace(protocol))
	return protocol == "TCP" || protocol == "UDP"
}

func analyticsStreamProtocolMatches(stream npm.Stream, protocol string) bool {
	switch strings.ToUpper(strings.TrimSpace(protocol)) {
	case "TCP":
		return stream.Tcp_forwarding
	case "UDP":
		return stream.Udp_forwarding
	default:
		return false
	}
}

type analyticsStreamEvidence uint8

const (
	analyticsEvidenceMismatch analyticsStreamEvidence = iota
	analyticsEvidenceMatch
	analyticsEvidenceIndeterminate
)

func analyticsStreamHostEvidence(configuredHost string, destination analyticsEndpoint, aliases map[string][]string) analyticsStreamEvidence {
	configuredHost = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(configuredHost, "]"), "["))
	if configuredHost == "" {
		return analyticsEvidenceIndeterminate
	}
	if configuredIP := net.ParseIP(configuredHost); configuredIP != nil {
		if destination.IP == "" {
			return analyticsEvidenceIndeterminate
		}
		if configuredIP.String() == destination.IP {
			return analyticsEvidenceMatch
		}
		return analyticsEvidenceMismatch
	}
	parts := parseAnalyticsEndpoint(destination.RawAddress)
	if destination.IP == "" && strings.EqualFold(configuredHost, parts.host) {
		return analyticsEvidenceMatch
	}
	for _, alias := range aliases[destination.IP] {
		configured := strings.ToLower(configuredHost)
		managedAlias := strings.ToLower(alias)
		if configured == managedAlias || strings.HasSuffix(configured, "."+managedAlias) {
			return analyticsEvidenceMatch
		}
	}
	return analyticsEvidenceIndeterminate
}

func analyticsBreakdownSlice(values map[string]*analyticsBreakdownItem, limit int) ([]analyticsBreakdownItem, int) {
	result := make([]analyticsBreakdownItem, 0, len(values))
	for _, item := range values {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].TotalBytes != result[j].TotalBytes {
			return result[i].TotalBytes > result[j].TotalBytes
		}
		if result[i].Connections != result[j].Connections {
			return result[i].Connections > result[j].Connections
		}
		return result[i].Value < result[j].Value
	})
	available := len(result)
	if limit >= 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, available
}

func capAnalyticsDestinations(destinations []analyticsDestination, limit int) ([]analyticsDestination, int) {
	available := len(destinations)
	if limit >= 0 && len(destinations) > limit {
		return destinations[:limit], available
	}
	return destinations, available
}

func capLatestAnalyticsTimeline(points []analyticsTimelinePoint, limit int) ([]analyticsTimelinePoint, int) {
	available := len(points)
	if limit >= 0 && len(points) > limit {
		return points[len(points)-limit:], available
	}
	return points, available
}

func analyticsCollectionTruncated(available, returned int) bool {
	return available > returned
}

func analyticsPathID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "path_" + hex.EncodeToString(sum[:8])
}

func analyticsPort(raw string) string {
	port := parseAnalyticsEndpoint(raw).port
	if port == "" {
		return "unknown"
	}
	return port
}

func analyticsCountry(country string) string {
	country = strings.TrimSpace(country)
	if country == "" {
		return "Unknown"
	}
	return country
}

func analyticsOutcome(status int) string {
	if status == http.StatusOK {
		return "success"
	}
	return "failed"
}

func analyticsFailureRate(metrics analyticsAccumulator) float64 {
	if metrics.connections == 0 {
		return 0
	}
	return float64(metrics.failures) / float64(metrics.connections)
}

func analyticsAverageSession(metrics analyticsAccumulator) float64 {
	if metrics.connections == 0 {
		return 0
	}
	return metrics.sessionSum / float64(metrics.connections)
}

func analyticsTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func analyticsSessionKey(entry streamLogEntry) string {
	return strings.Join([]string{entry.Timestamp, entry.ClientIP, entry.ProxyAddr, entry.UpstreamAddr, entry.Protocol, strconv.Itoa(entry.Status)}, "\x00")
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
