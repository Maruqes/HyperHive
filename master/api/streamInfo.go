package api

import (
	"512SvMan/db"
	"bufio"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oschwald/geoip2-golang"
)

const (
	streamLogPath     = "npm-data/logs/stream-proxy.log"
	streamTimeLayout  = "02/Jan/2006:15:04:05 -0700"
	geoIPDirectory    = "geoipdb"
	topTalkersLimit   = 5
	topCountryEntries = 5
)

var (
	streamLogPattern = regexp.MustCompile(`^(\S+)\s+\[([^\]]+)\]\s+(\S+)\s+(\d+)\s+(\S+)\s+(\S+)\s+(\S+)\s+\[([^\]]+)\]\s+->\s+(\S+)$`)
	preferredGeoIPDB = []string{
		"GeoLite2-City.mmdb",
		"GeoLite2-Country.mmdb",
		"GeoIP2-City.mmdb",
		"GeoIP2-Country.mmdb",
	}
	errGeoIPUnavailable = errors.New("geoip database not found")
)

type streamLogEntry struct {
	ClientIP      string    `json:"client_ip"`
	Timestamp     string    `json:"timestamp"`
	Protocol      string    `json:"protocol"`
	Status        int       `json:"status"`
	BytesSent     int64     `json:"bytes_sent"`
	BytesReceived int64     `json:"bytes_received"`
	SessionTime   float64   `json:"session_time_seconds"`
	ProxyAddr     string    `json:"proxy_addr"`
	UpstreamAddr  string    `json:"upstream_addr"`
	Time          time.Time `json:"-"`
}

type streamLogResponse struct {
	Count   int              `json:"count"`
	Entries []streamLogEntry `json:"entries"`
}

type streamSummaryResponse struct {
	TotalConnections      int                   `json:"total_connections"`
	UniqueClients         int                   `json:"unique_clients"`
	TotalBytesSent        int64                 `json:"total_bytes_sent"`
	TotalBytesReceived    int64                 `json:"total_bytes_received"`
	AvgSessionSeconds     float64               `json:"avg_session_seconds"`
	ConnectionsPerMinute  float64               `json:"connections_per_minute"`
	Window                analyticsWindow       `json:"window"`
	TopTalkers            []talkerStat          `json:"top_talkers"`
	TopCountries          []db.CountryBreakdown `json:"top_countries"`
	TopCountryVisitors    int                   `json:"top_country_visitors"`
	TotalTrackedCountries int                   `json:"total_tracked_countries"`
}

type analyticsWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type talkerStat struct {
	ClientIP      string        `json:"client_ip"`
	Connections   int           `json:"connections"`
	BytesSent     int64         `json:"bytes_sent"`
	BytesReceived int64         `json:"bytes_received"`
	Location      *locationInfo `json:"location,omitempty"`
}

type locationInfo struct {
	Country    string  `json:"country"`
	CountryISO string  `json:"country_iso"`
	City       string  `json:"city,omitempty"`
	Latitude   float64 `json:"latitude,omitempty"`
	Longitude  float64 `json:"longitude,omitempty"`
}

type dailyVisitorsResponse struct {
	Day               string                `json:"day"`
	UniqueVisitors    int                   `json:"unique_visitors"`
	TotalConnections  int                   `json:"total_connections"`
	BytesSent         int64                 `json:"bytes_sent"`
	BytesReceived     int64                 `json:"bytes_received"`
	AvgSessionSeconds float64               `json:"avg_session_seconds"`
	CountryBreakdown  []db.CountryBreakdown `json:"country_breakdown"`
	Change            dailyChangeStats      `json:"change_vs_previous"`
}

type dailyChangeStats struct {
	UniqueVisitorsPct float64 `json:"unique_visitors_pct"`
	BytesSentPct      float64 `json:"bytes_sent_pct"`
	BytesRecvPct      float64 `json:"bytes_received_pct"`
}

// New panel response types

type allCountriesResponse struct {
	TotalCountries int                   `json:"total_countries"`
	TotalVisitors  int                   `json:"total_visitors"`
	Countries      []db.CountryBreakdown `json:"countries"`
}

type protocolStatsResponse struct {
	TotalConnections int            `json:"total_connections"`
	Protocols        []protocolStat `json:"protocols"`
}

type protocolStat struct {
	Protocol    string  `json:"protocol"`
	Connections int     `json:"connections"`
	BytesSent   int64   `json:"bytes_sent"`
	BytesRecv   int64   `json:"bytes_received"`
	AvgSession  float64 `json:"avg_session_seconds"`
	Percentage  float64 `json:"percentage"`
}

type hourlyActivityResponse struct {
	Hours []hourlyActivity `json:"hours"`
}

type hourlyActivity struct {
	Hour           int     `json:"hour"`
	Connections    int     `json:"connections"`
	UniqueIPs      int     `json:"unique_ips"`
	BytesSent      int64   `json:"bytes_sent"`
	BytesReceived  int64   `json:"bytes_received"`
	AvgSessionTime float64 `json:"avg_session_seconds"`
}

type upstreamStatsResponse struct {
	TotalUpstreams int            `json:"total_upstreams"`
	Upstreams      []upstreamStat `json:"upstreams"`
}

type upstreamStat struct {
	UpstreamAddr  string  `json:"upstream_addr"`
	Connections   int     `json:"connections"`
	BytesSent     int64   `json:"bytes_sent"`
	BytesReceived int64   `json:"bytes_received"`
	AvgSession    float64 `json:"avg_session_seconds"`
	UniqueClients int     `json:"unique_clients"`
	Percentage    float64 `json:"percentage"`
}

type geographicHeatmapResponse struct {
	TotalPoints int            `json:"total_points"`
	Points      []geoHeatPoint `json:"points"`
}

type geoHeatPoint struct {
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Country     string  `json:"country"`
	CountryISO  string  `json:"country_iso"`
	City        string  `json:"city,omitempty"`
	Connections int     `json:"connections"`
	Weight      float64 `json:"weight"`
}

type sessionDistributionResponse struct {
	Ranges []sessionRange `json:"ranges"`
}

type sessionRange struct {
	Label      string  `json:"label"`
	MinSeconds float64 `json:"min_seconds"`
	MaxSeconds float64 `json:"max_seconds"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
	TotalBytes int64   `json:"total_bytes"`
}

// dayAccumulator keeps track of per-day stats before rendering.
type dayAccumulator struct {
	UniqueIPs        map[string]struct{}
	TotalConnections int
	BytesSent        int64
	BytesReceived    int64
	SessionSeconds   float64
}

type ipAggregate struct {
	Connections    int
	BytesSent      int64
	BytesReceived  int64
	SessionSeconds float64
	Location       *locationInfo
}

type streamAggregator struct {
	resolver         *geoResolver
	totalConnections int
	totalBytesSent   int64
	totalBytesRecv   int64
	totalSession     float64
	windowStart      time.Time
	windowEnd        time.Time
	uniqueIPs        map[string]struct{}
	ipTotals         map[string]*ipAggregate
	dailyStats       map[string]*dayAccumulator
}

type geoResolver struct {
	reader *geoip2.Reader
	cache  map[string]*locationInfo
}

type countryAggregate struct {
	Country string
	ISO     string
	Count   int
}

//exmample of streams.log
/*
192.168.1.69 [06/Nov/2025:13:58:36 +0000] TCP 200 130 33 0.337 [192.168.1.175:25565] -> 192.168.76.77:25565
192.168.1.69 [06/Nov/2025:14:06:31 +0000] TCP 200 134731152 223110 470.999 [192.168.1.175:25565] -> 192.168.76.77:25565
10.8.0.2 [07/Nov/2025:10:58:31 +0000] TCP 200 194 907 0.003 [192.168.1.175:36002] -> 192.168.76.55:36001
10.8.0.2 [07/Nov/2025:10:58:31 +0000] TCP 200 194 929 0.001 [192.168.1.175:36002] -> 192.168.76.55:36001
10.8.0.2 [07/Nov/2025:10:58:36 +0000] TCP 200 194 907 0.001 [192.168.1.175:36002] -> 192.168.76.55:36001
10.8.0.2 [07/Nov/2025:10:58:36 +0000] TCP 200 194 929 0.072 [192.168.1.175:36002] -> 192.168.76.55:36001
10.8.0.2 [07/Nov/2025:11:38:45 +0000] TCP 200 194 907 0.013 [192.168.1.175:36002] -> 192.168.76.55:36001
10.8.0.2 [07/Nov/2025:11:38:45 +0000] TCP 200 194 929 0.001 [192.168.1.175:36002] -> 192.168.76.55:36001
*/

// file should be inside "npm-data/logs/stream-proxy.log"
func getData(w http.ResponseWriter, r *http.Request) {
	entries, err := readStreamLogEntries()
	if err != nil {
		if os.IsNotExist(err) {
			respondJSONError(w, http.StatusNotFound, "stream-proxy.log not found")
			return
		}
		respondJSONError(w, http.StatusInternalServerError, "failed to read stream-proxy.log")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(streamLogResponse{
		Count:   len(entries),
		Entries: entries,
	})
}

func getStreamSummary(w http.ResponseWriter, r *http.Request) {
	summary, _, err := prepareStreamAnalytics()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			respondJSONError(w, http.StatusNotFound, "stream-proxy.log not found")
			return
		}
		respondJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(summary)
}

func getDailyVisitorStats(w http.ResponseWriter, r *http.Request) {
	_, daily, err := prepareStreamAnalytics()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			respondJSONError(w, http.StatusNotFound, "stream-proxy.log not found")
			return
		}
		respondJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(daily)
}

// New panel handlers

func getAllCountries(w http.ResponseWriter, r *http.Request) {
	entries, err := readStreamLogEntries()
	if err != nil {
		if os.IsNotExist(err) {
			respondJSONError(w, http.StatusNotFound, "stream-proxy.log not found")
			return
		}
		respondJSONError(w, http.StatusInternalServerError, "failed to read stream-proxy.log")
		return
	}

	response := computeAllCountries(entries)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func getProtocolStats(w http.ResponseWriter, r *http.Request) {
	entries, err := readStreamLogEntries()
	if err != nil {
		if os.IsNotExist(err) {
			respondJSONError(w, http.StatusNotFound, "stream-proxy.log not found")
			return
		}
		respondJSONError(w, http.StatusInternalServerError, "failed to read stream-proxy.log")
		return
	}

	response := computeProtocolStats(entries)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func getHourlyActivity(w http.ResponseWriter, r *http.Request) {
	entries, err := readStreamLogEntries()
	if err != nil {
		if os.IsNotExist(err) {
			respondJSONError(w, http.StatusNotFound, "stream-proxy.log not found")
			return
		}
		respondJSONError(w, http.StatusInternalServerError, "failed to read stream-proxy.log")
		return
	}

	response := computeHourlyActivity(entries)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func getUpstreamStats(w http.ResponseWriter, r *http.Request) {
	entries, err := readStreamLogEntries()
	if err != nil {
		if os.IsNotExist(err) {
			respondJSONError(w, http.StatusNotFound, "stream-proxy.log not found")
			return
		}
		respondJSONError(w, http.StatusInternalServerError, "failed to read stream-proxy.log")
		return
	}

	response := computeUpstreamStats(entries)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func getGeographicHeatmap(w http.ResponseWriter, r *http.Request) {
	entries, err := readStreamLogEntries()
	if err != nil {
		if os.IsNotExist(err) {
			respondJSONError(w, http.StatusNotFound, "stream-proxy.log not found")
			return
		}
		respondJSONError(w, http.StatusInternalServerError, "failed to read stream-proxy.log")
		return
	}

	response := computeGeographicHeatmap(entries)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func getSessionDistribution(w http.ResponseWriter, r *http.Request) {
	entries, err := readStreamLogEntries()
	if err != nil {
		if os.IsNotExist(err) {
			respondJSONError(w, http.StatusNotFound, "stream-proxy.log not found")
			return
		}
		respondJSONError(w, http.StatusInternalServerError, "failed to read stream-proxy.log")
		return
	}

	response := computeSessionDistribution(entries)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func prepareStreamAnalytics() (streamSummaryResponse, []dailyVisitorsResponse, error) {
	entries, err := readStreamLogEntries()
	if err != nil {
		return streamSummaryResponse{}, nil, err
	}
	return computeStreamAnalytics(entries)
}

// Computation functions for new panels

func computeAllCountries(entries []streamLogEntry) allCountriesResponse {
	resolver := newGeoResolver()
	uniqueIPs := make(map[string]struct{})
	ipToLocation := make(map[string]*locationInfo)

	for _, entry := range entries {
		uniqueIPs[entry.ClientIP] = struct{}{}
		if _, exists := ipToLocation[entry.ClientIP]; !exists {
			if loc, ok := resolver.Lookup(entry.ClientIP); ok {
				ipToLocation[entry.ClientIP] = loc
			}
		}
	}

	counter := make(map[string]*countryAggregate)
	for ip := range uniqueIPs {
		country, iso := "Unknown", "??"
		if loc, ok := ipToLocation[ip]; ok && loc != nil {
			if loc.Country != "" {
				country = loc.Country
			}
			if loc.CountryISO != "" {
				iso = loc.CountryISO
			}
		}
		key := iso + "::" + country
		item := counter[key]
		if item == nil {
			item = &countryAggregate{Country: country, ISO: iso}
			counter[key] = item
		}
		item.Count++
	}

	result := make([]db.CountryBreakdown, 0, len(counter))
	totalVisitors := len(uniqueIPs)
	for _, item := range counter {
		result = append(result, db.CountryBreakdown{
			Country:    item.Country,
			ISOCode:    item.ISO,
			Visitors:   item.Count,
			Percentage: percentFloat(item.Count, totalVisitors),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Visitors == result[j].Visitors {
			return result[i].Country < result[j].Country
		}
		return result[i].Visitors > result[j].Visitors
	})

	return allCountriesResponse{
		TotalCountries: len(result),
		TotalVisitors:  totalVisitors,
		Countries:      result,
	}
}

func computeProtocolStats(entries []streamLogEntry) protocolStatsResponse {
	protocolData := make(map[string]*protocolStat)
	totalConnections := len(entries)

	for _, entry := range entries {
		stat, exists := protocolData[entry.Protocol]
		if !exists {
			stat = &protocolStat{Protocol: entry.Protocol}
			protocolData[entry.Protocol] = stat
		}
		stat.Connections++
		stat.BytesSent += entry.BytesSent
		stat.BytesRecv += entry.BytesReceived
		stat.AvgSession += entry.SessionTime
	}

	results := make([]protocolStat, 0, len(protocolData))
	for _, stat := range protocolData {
		if stat.Connections > 0 {
			stat.AvgSession = roundFloat(stat.AvgSession / float64(stat.Connections))
		}
		stat.Percentage = percentFloat(stat.Connections, totalConnections)
		results = append(results, *stat)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Connections > results[j].Connections
	})

	return protocolStatsResponse{
		TotalConnections: totalConnections,
		Protocols:        results,
	}
}

func computeHourlyActivity(entries []streamLogEntry) hourlyActivityResponse {
	hourlyData := make(map[int]*struct {
		connections   int
		uniqueIPs     map[string]struct{}
		bytesSent     int64
		bytesReceived int64
		sessionTime   float64
	})

	for i := 0; i < 24; i++ {
		hourlyData[i] = &struct {
			connections   int
			uniqueIPs     map[string]struct{}
			bytesSent     int64
			bytesReceived int64
			sessionTime   float64
		}{
			uniqueIPs: make(map[string]struct{}),
		}
	}

	for _, entry := range entries {
		if entry.Time.IsZero() {
			continue
		}
		hour := entry.Time.Hour()
		data := hourlyData[hour]
		data.connections++
		data.uniqueIPs[entry.ClientIP] = struct{}{}
		data.bytesSent += entry.BytesSent
		data.bytesReceived += entry.BytesReceived
		data.sessionTime += entry.SessionTime
	}

	results := make([]hourlyActivity, 0, 24)
	for hour := 0; hour < 24; hour++ {
		data := hourlyData[hour]
		avgSession := 0.0
		if data.connections > 0 {
			avgSession = roundFloat(data.sessionTime / float64(data.connections))
		}
		results = append(results, hourlyActivity{
			Hour:           hour,
			Connections:    data.connections,
			UniqueIPs:      len(data.uniqueIPs),
			BytesSent:      data.bytesSent,
			BytesReceived:  data.bytesReceived,
			AvgSessionTime: avgSession,
		})
	}

	return hourlyActivityResponse{Hours: results}
}

func computeUpstreamStats(entries []streamLogEntry) upstreamStatsResponse {
	upstreamData := make(map[string]*struct {
		connections   int
		bytesSent     int64
		bytesReceived int64
		sessionTime   float64
		uniqueClients map[string]struct{}
	})

	totalConnections := len(entries)

	for _, entry := range entries {
		data, exists := upstreamData[entry.UpstreamAddr]
		if !exists {
			data = &struct {
				connections   int
				bytesSent     int64
				bytesReceived int64
				sessionTime   float64
				uniqueClients map[string]struct{}
			}{
				uniqueClients: make(map[string]struct{}),
			}
			upstreamData[entry.UpstreamAddr] = data
		}
		data.connections++
		data.bytesSent += entry.BytesSent
		data.bytesReceived += entry.BytesReceived
		data.sessionTime += entry.SessionTime
		data.uniqueClients[entry.ClientIP] = struct{}{}
	}

	results := make([]upstreamStat, 0, len(upstreamData))
	for upstream, data := range upstreamData {
		avgSession := 0.0
		if data.connections > 0 {
			avgSession = roundFloat(data.sessionTime / float64(data.connections))
		}
		results = append(results, upstreamStat{
			UpstreamAddr:  upstream,
			Connections:   data.connections,
			BytesSent:     data.bytesSent,
			BytesReceived: data.bytesReceived,
			AvgSession:    avgSession,
			UniqueClients: len(data.uniqueClients),
			Percentage:    percentFloat(data.connections, totalConnections),
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Connections > results[j].Connections
	})

	return upstreamStatsResponse{
		TotalUpstreams: len(results),
		Upstreams:      results,
	}
}

func computeGeographicHeatmap(entries []streamLogEntry) geographicHeatmapResponse {
	resolver := newGeoResolver()
	locationData := make(map[string]*struct {
		location    *locationInfo
		connections int
	})

	for _, entry := range entries {
		if loc, ok := resolver.Lookup(entry.ClientIP); ok && loc != nil {
			if loc.Latitude == 0 && loc.Longitude == 0 {
				continue
			}
			key := loc.Country + "::" + loc.City
			data, exists := locationData[key]
			if !exists {
				data = &struct {
					location    *locationInfo
					connections int
				}{
					location: loc,
				}
				locationData[key] = data
			}
			data.connections++
		}
	}

	maxConnections := 0
	for _, data := range locationData {
		if data.connections > maxConnections {
			maxConnections = data.connections
		}
	}

	results := make([]geoHeatPoint, 0, len(locationData))
	for _, data := range locationData {
		weight := 0.0
		if maxConnections > 0 {
			weight = roundFloat(float64(data.connections) / float64(maxConnections))
		}
		results = append(results, geoHeatPoint{
			Latitude:    data.location.Latitude,
			Longitude:   data.location.Longitude,
			Country:     data.location.Country,
			CountryISO:  data.location.CountryISO,
			City:        data.location.City,
			Connections: data.connections,
			Weight:      weight,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Connections > results[j].Connections
	})

	return geographicHeatmapResponse{
		TotalPoints: len(results),
		Points:      results,
	}
}

func computeSessionDistribution(entries []streamLogEntry) sessionDistributionResponse {
	ranges := []struct {
		label      string
		minSeconds float64
		maxSeconds float64
	}{
		{"< 1 second", 0, 1},
		{"1-5 seconds", 1, 5},
		{"5-30 seconds", 5, 30},
		{"30-60 seconds", 30, 60},
		{"1-5 minutes", 60, 300},
		{"5-15 minutes", 300, 900},
		{"15-30 minutes", 900, 1800},
		{"30-60 minutes", 1800, 3600},
		{"> 1 hour", 3600, math.MaxFloat64},
	}

	rangeCounts := make([]int, len(ranges))
	rangeBytes := make([]int64, len(ranges))

	for _, entry := range entries {
		for i, r := range ranges {
			if entry.SessionTime >= r.minSeconds && entry.SessionTime < r.maxSeconds {
				rangeCounts[i]++
				rangeBytes[i] += entry.BytesSent + entry.BytesReceived
				break
			}
		}
	}

	totalEntries := len(entries)
	results := make([]sessionRange, 0, len(ranges))
	for i, r := range ranges {
		results = append(results, sessionRange{
			Label:      r.label,
			MinSeconds: r.minSeconds,
			MaxSeconds: r.maxSeconds,
			Count:      rangeCounts[i],
			Percentage: percentFloat(rangeCounts[i], totalEntries),
			TotalBytes: rangeBytes[i],
		})
	}

	return sessionDistributionResponse{Ranges: results}
}

func computeStreamAnalytics(entries []streamLogEntry) (streamSummaryResponse, []dailyVisitorsResponse, error) {
	aggregator := newStreamAggregator()
	for _, entry := range entries {
		aggregator.absorb(entry)
	}
	summary := aggregator.summary()
	daily := aggregator.dailyResponses()
	persistDailyMetrics(daily)
	return summary, daily, nil
}

func persistDailyMetrics(daily []dailyVisitorsResponse) {
	if len(daily) == 0 {
		return
	}
	payload := make([]db.DailyStreamMetric, 0, len(daily))
	for _, day := range daily {
		payload = append(payload, db.DailyStreamMetric{
			Day:               day.Day,
			UniqueVisitors:    day.UniqueVisitors,
			TotalConnections:  day.TotalConnections,
			BytesSent:         day.BytesSent,
			BytesReceived:     day.BytesReceived,
			AvgSessionSeconds: day.AvgSessionSeconds,
			CountryBreakdown:  day.CountryBreakdown,
		})
	}
	if err := db.UpsertStreamDailyMetrics(payload); err != nil {
		log.Printf("stream analytics: persist daily metrics: %v", err)
	}
}

func newStreamAggregator() *streamAggregator {
	return &streamAggregator{
		resolver:   newGeoResolver(),
		uniqueIPs:  make(map[string]struct{}),
		ipTotals:   make(map[string]*ipAggregate),
		dailyStats: make(map[string]*dayAccumulator),
	}
}

func (a *streamAggregator) absorb(entry streamLogEntry) {
	a.totalConnections++
	a.totalBytesSent += entry.BytesSent
	a.totalBytesRecv += entry.BytesReceived
	a.totalSession += entry.SessionTime

	ipAgg, ok := a.ipTotals[entry.ClientIP]
	if !ok {
		ipAgg = &ipAggregate{}
		a.ipTotals[entry.ClientIP] = ipAgg
	}
	ipAgg.Connections++
	ipAgg.BytesSent += entry.BytesSent
	ipAgg.BytesReceived += entry.BytesReceived
	ipAgg.SessionSeconds += entry.SessionTime

	if ipAgg.Location == nil {
		if loc, ok := a.resolver.Lookup(entry.ClientIP); ok {
			ipAgg.Location = loc
		}
	}

	a.uniqueIPs[entry.ClientIP] = struct{}{}

	if !entry.Time.IsZero() {
		if a.windowStart.IsZero() || entry.Time.Before(a.windowStart) {
			a.windowStart = entry.Time
		}
		if entry.Time.After(a.windowEnd) {
			a.windowEnd = entry.Time
		}

		dayKey := entry.Time.UTC().Format("2006-01-02")
		day := a.dailyStats[dayKey]
		if day == nil {
			day = &dayAccumulator{
				UniqueIPs: make(map[string]struct{}),
			}
			a.dailyStats[dayKey] = day
		}
		day.TotalConnections++
		day.BytesSent += entry.BytesSent
		day.BytesReceived += entry.BytesReceived
		day.SessionSeconds += entry.SessionTime
		day.UniqueIPs[entry.ClientIP] = struct{}{}
	}
}

func (a *streamAggregator) summary() streamSummaryResponse {
	summary := streamSummaryResponse{
		TotalConnections:      a.totalConnections,
		UniqueClients:         len(a.uniqueIPs),
		TotalBytesSent:        a.totalBytesSent,
		TotalBytesReceived:    a.totalBytesRecv,
		TopTalkers:            []talkerStat{},
		TopCountries:          []db.CountryBreakdown{},
		TopCountryVisitors:    0,
		TotalTrackedCountries: 0,
		Window: analyticsWindow{
			Start: a.windowStart,
			End:   a.windowEnd,
		},
	}

	if a.totalConnections > 0 {
		summary.AvgSessionSeconds = roundFloat(a.totalSession / float64(a.totalConnections))
	}
	summary.ConnectionsPerMinute = calcRatePerMinute(a.totalConnections, a.windowStart, a.windowEnd)
	summary.TopTalkers = buildTopTalkers(a.ipTotals)

	countries, totalVisitors, countryCount := buildCountryBreakdown(a.ipTotals, a.uniqueIPs)
	summary.TopCountries = countries
	summary.TopCountryVisitors = totalVisitors
	summary.TotalTrackedCountries = countryCount

	return summary
}

func (a *streamAggregator) dailyResponses() []dailyVisitorsResponse {
	if len(a.dailyStats) == 0 {
		return []dailyVisitorsResponse{}
	}
	days := make([]string, 0, len(a.dailyStats))
	for day := range a.dailyStats {
		days = append(days, day)
	}
	sort.Strings(days)

	results := make([]dailyVisitorsResponse, 0, len(days))
	var prev *dailyVisitorsResponse

	for _, day := range days {
		stats := a.dailyStats[day]
		resp := dailyVisitorsResponse{
			Day:              day,
			UniqueVisitors:   len(stats.UniqueIPs),
			TotalConnections: stats.TotalConnections,
			BytesSent:        stats.BytesSent,
			BytesReceived:    stats.BytesReceived,
		}
		if stats.TotalConnections > 0 {
			resp.AvgSessionSeconds = roundFloat(stats.SessionSeconds / float64(stats.TotalConnections))
		}
		resp.CountryBreakdown = buildCountryBreakdownForIPs(stats.UniqueIPs, a.ipTotals)
		if prev != nil {
			resp.Change = calcDailyChange(resp, *prev)
		}
		results = append(results, resp)
		prev = &results[len(results)-1]
	}

	return reverseDailyVisitors(results)
}

func buildTopTalkers(totals map[string]*ipAggregate) []talkerStat {
	if len(totals) == 0 {
		return []talkerStat{}
	}
	items := make([]talkerStat, 0, len(totals))
	for ip, agg := range totals {
		items = append(items, talkerStat{
			ClientIP:      ip,
			Connections:   agg.Connections,
			BytesSent:     agg.BytesSent,
			BytesReceived: agg.BytesReceived,
			Location:      agg.Location,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		left := items[i].BytesSent + items[i].BytesReceived
		right := items[j].BytesSent + items[j].BytesReceived
		if left == right {
			return items[i].Connections > items[j].Connections
		}
		return left > right
	})
	if len(items) > topTalkersLimit {
		items = items[:topTalkersLimit]
	}
	return items
}

func buildCountryBreakdown(totals map[string]*ipAggregate, uniqueIPs map[string]struct{}) ([]db.CountryBreakdown, int, int) {
	totalVisitors := len(uniqueIPs)
	if totalVisitors == 0 {
		return []db.CountryBreakdown{}, 0, 0
	}

	counter := make(map[string]*countryAggregate)
	for ip := range uniqueIPs {
		agg := totals[ip]
		country, iso := "Unknown", "??"
		if agg != nil && agg.Location != nil {
			if agg.Location.Country != "" {
				country = agg.Location.Country
			}
			if agg.Location.CountryISO != "" {
				iso = agg.Location.CountryISO
			}
		}
		key := iso + "::" + country
		item := counter[key]
		if item == nil {
			item = &countryAggregate{Country: country, ISO: iso}
			counter[key] = item
		}
		item.Count++
	}

	result := make([]db.CountryBreakdown, 0, len(counter))
	for _, item := range counter {
		result = append(result, db.CountryBreakdown{
			Country:    item.Country,
			ISOCode:    item.ISO,
			Visitors:   item.Count,
			Percentage: percentFloat(item.Count, totalVisitors),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Visitors == result[j].Visitors {
			return result[i].Country < result[j].Country
		}
		return result[i].Visitors > result[j].Visitors
	})

	totalCountries := len(result)
	if len(result) > topCountryEntries {
		result = result[:topCountryEntries]
	}

	return result, totalVisitors, totalCountries
}

func buildCountryBreakdownForIPs(ips map[string]struct{}, totals map[string]*ipAggregate) []db.CountryBreakdown {
	if len(ips) == 0 {
		return []db.CountryBreakdown{}
	}
	counter := make(map[string]*countryAggregate)
	for ip := range ips {
		agg := totals[ip]
		country, iso := "Unknown", "??"
		if agg != nil && agg.Location != nil {
			if agg.Location.Country != "" {
				country = agg.Location.Country
			}
			if agg.Location.CountryISO != "" {
				iso = agg.Location.CountryISO
			}
		}
		key := iso + "::" + country
		item := counter[key]
		if item == nil {
			item = &countryAggregate{Country: country, ISO: iso}
			counter[key] = item
		}
		item.Count++
	}

	result := make([]db.CountryBreakdown, 0, len(counter))
	totalVisitors := len(ips)
	for _, item := range counter {
		result = append(result, db.CountryBreakdown{
			Country:    item.Country,
			ISOCode:    item.ISO,
			Visitors:   item.Count,
			Percentage: percentFloat(item.Count, totalVisitors),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Visitors == result[j].Visitors {
			return result[i].Country < result[j].Country
		}
		return result[i].Visitors > result[j].Visitors
	})
	if len(result) > topCountryEntries {
		result = result[:topCountryEntries]
	}
	return result
}

func calcRatePerMinute(total int, start, end time.Time) float64 {
	if total == 0 || start.IsZero() || end.IsZero() {
		return 0
	}
	diff := end.Sub(start).Minutes()
	if diff <= 0 {
		return float64(total)
	}
	return roundFloat(float64(total) / diff)
}

func calcDailyChange(current, prev dailyVisitorsResponse) dailyChangeStats {
	return dailyChangeStats{
		UniqueVisitorsPct: percentageDelta(current.UniqueVisitors, prev.UniqueVisitors),
		BytesSentPct:      percentageDeltaFloat(float64(current.BytesSent), float64(prev.BytesSent)),
		BytesRecvPct:      percentageDeltaFloat(float64(current.BytesReceived), float64(prev.BytesReceived)),
	}
}

func reverseDailyVisitors(items []dailyVisitorsResponse) []dailyVisitorsResponse {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items
}

func roundFloat(value float64) float64 {
	return math.Round(value*100) / 100
}

func percentFloat(value, total int) float64 {
	if total == 0 {
		return 0
	}
	return roundFloat(float64(value) / float64(total) * 100)
}

func percentageDelta(current, previous int) float64 {
	return percentageDeltaFloat(float64(current), float64(previous))
}

func percentageDeltaFloat(current, previous float64) float64 {
	if previous == 0 {
		if current == 0 {
			return 0
		}
		return 100
	}
	return roundFloat((current - previous) / previous * 100)
}

func readStreamLogEntries() ([]streamLogEntry, error) {
	file, err := os.Open(streamLogPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	entries := make([]streamLogEntry, 0, 128)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		entry, ok := parseStreamLogLine(line)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func parseStreamLogLine(line string) (streamLogEntry, bool) {
	match := streamLogPattern.FindStringSubmatch(line)
	if match == nil {
		return streamLogEntry{}, false
	}

	status, err := strconv.Atoi(match[4])
	if err != nil {
		return streamLogEntry{}, false
	}

	bytesSent, err := parseIntField(match[5])
	if err != nil {
		return streamLogEntry{}, false
	}

	bytesReceived, err := parseIntField(match[6])
	if err != nil {
		return streamLogEntry{}, false
	}

	sessionTime, err := parseFloatField(match[7])
	if err != nil {
		return streamLogEntry{}, false
	}

	timestamp := match[2]
	formattedTime := timestamp
	var parsedTime time.Time
	if parsed, err := time.Parse(streamTimeLayout, timestamp); err == nil {
		formattedTime = parsed.Format(time.RFC3339)
		parsedTime = parsed
	}

	return streamLogEntry{
		ClientIP:      match[1],
		Timestamp:     formattedTime,
		Protocol:      match[3],
		Status:        status,
		BytesSent:     bytesSent,
		BytesReceived: bytesReceived,
		SessionTime:   sessionTime,
		ProxyAddr:     match[8],
		UpstreamAddr:  match[9],
		Time:          parsedTime,
	}, true
}

func parseIntField(value string) (int64, error) {
	if value == "" || value == "-" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func parseFloatField(value string) (float64, error) {
	if value == "" || value == "-" {
		return 0, nil
	}
	return strconv.ParseFloat(value, 64)
}

func respondJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (g *geoResolver) Lookup(ip string) (*locationInfo, bool) {
	if g == nil {
		return nil, false
	}
	if loc, ok := g.cache[ip]; ok {
		if loc == nil {
			return nil, false
		}
		return loc, true
	}
	if g.reader == nil {
		g.cache[ip] = nil
		return nil, false
	}
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		g.cache[ip] = nil
		return nil, false
	}
	if loc, err := g.lookupCity(parsedIP); err == nil {
		g.cache[ip] = loc
		return loc, true
	}
	if loc, err := g.lookupCountry(parsedIP); err == nil {
		g.cache[ip] = loc
		return loc, true
	}
	g.cache[ip] = nil
	return nil, false
}

func newGeoResolver() *geoResolver {
	path, err := findGeoIPDatabase()
	if err != nil {
		return &geoResolver{cache: make(map[string]*locationInfo)}
	}
	reader, err := geoip2.Open(path)
	if err != nil {
		log.Printf("geoip: open %s failed: %v", path, err)
		return &geoResolver{cache: make(map[string]*locationInfo)}
	}
	return &geoResolver{
		reader: reader,
		cache:  make(map[string]*locationInfo),
	}
}

func findGeoIPDatabase() (string, error) {
	dir := filepath.Clean(geoIPDirectory)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	for _, name := range preferredGeoIPDB {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}

	candidates, err := filepath.Glob(filepath.Join(dir, "*.mmdb"))
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", errGeoIPUnavailable
	}
	sort.Strings(candidates)
	return candidates[0], nil
}

func (g *geoResolver) lookupCity(ip net.IP) (*locationInfo, error) {
	if g.reader == nil {
		return nil, errors.New("geoip reader not initialized")
	}
	record, err := g.reader.City(ip)
	if err != nil {
		return nil, err
	}
	loc := &locationInfo{
		Country:    record.Country.Names["en"],
		CountryISO: record.Country.IsoCode,
		City:       record.City.Names["en"],
		Latitude:   record.Location.Latitude,
		Longitude:  record.Location.Longitude,
	}
	if loc.Country == "" && record.Country.IsoCode != "" {
		loc.Country = record.Country.IsoCode
	}
	normalizeLocation(loc)
	return loc, nil
}

func (g *geoResolver) lookupCountry(ip net.IP) (*locationInfo, error) {
	if g.reader == nil {
		return nil, errors.New("geoip reader not initialized")
	}
	record, err := g.reader.Country(ip)
	if err != nil {
		return nil, err
	}
	loc := &locationInfo{
		Country:    record.Country.Names["en"],
		CountryISO: record.Country.IsoCode,
	}
	if loc.Country == "" && record.Country.IsoCode != "" {
		loc.Country = record.Country.IsoCode
	}
	normalizeLocation(loc)
	return loc, nil
}

func normalizeLocation(loc *locationInfo) {
	if loc.Country == "" {
		loc.Country = "Unknown"
	}
	if loc.CountryISO == "" {
		loc.CountryISO = "??"
	}
}

func setupStreamInfo(r chi.Router) chi.Router {
	return r.Route("/streamInfo", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "static/streamInfo.html")
		})
		r.Get("/data", getData)
		r.Get("/summary", getStreamSummary)
		r.Get("/visitors", getDailyVisitorStats)
		// New panel endpoints
		r.Get("/countries", getAllCountries)
		r.Get("/protocols", getProtocolStats)
		r.Get("/hourly", getHourlyActivity)
		r.Get("/upstreams", getUpstreamStats)
		r.Get("/heatmap", getGeographicHeatmap)
		r.Get("/sessions", getSessionDistribution)
	})
}
