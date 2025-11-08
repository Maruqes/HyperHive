package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
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
	streamLogPath        = "npm-data/logs/stream-proxy.log"
	streamTimeLayout     = "02/Jan/2006:15:04:05 -0700"
	geoIPDirectory       = "geoipdb"
	topTalkersLimit      = 5
	topCountryEntries    = 5
	countryIPDetailLimit = 20
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
	Country       string    `json:"country,omitempty"`
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

// Summary statistics
type summaryStats struct {
	TotalConnections int     `json:"total_connections"`
	UniqueIPs        int     `json:"unique_ips"`
	TotalBytesSent   int64   `json:"total_bytes_sent"`
	TotalBytesRecv   int64   `json:"total_bytes_received"`
	TotalBytes       int64   `json:"total_bytes"`
	AvgSessionTime   float64 `json:"avg_session_time_seconds"`
	SuccessfulConns  int     `json:"successful_connections"`
	FailedConns      int     `json:"failed_connections"`
	UniqueCountries  int     `json:"unique_countries"`
	UniqueUpstreams  int     `json:"unique_upstreams"`
	DateRange        string  `json:"date_range"`
}

// Traffic by IP
type ipTrafficStats struct {
	IP             string  `json:"ip"`
	Country        string  `json:"country,omitempty"`
	Connections    int     `json:"connections"`
	BytesSent      int64   `json:"bytes_sent"`
	BytesReceived  int64   `json:"bytes_received"`
	TotalBytes     int64   `json:"total_bytes"`
	AvgSessionTime float64 `json:"avg_session_time_seconds"`
	FailedConns    int     `json:"failed_connections"`
}

// Traffic by Country
type countryTrafficStats struct {
	Country        string  `json:"country"`
	Connections    int     `json:"connections"`
	UniqueIPs      int     `json:"unique_ips"`
	BytesSent      int64   `json:"bytes_sent"`
	BytesReceived  int64   `json:"bytes_received"`
	TotalBytes     int64   `json:"total_bytes"`
	AvgSessionTime float64 `json:"avg_session_time_seconds"`
}

// Traffic by Port (upstream)
type portTrafficStats struct {
	Port           string  `json:"port"`
	Connections    int     `json:"connections"`
	UniqueIPs      int     `json:"unique_ips"`
	BytesSent      int64   `json:"bytes_sent"`
	BytesReceived  int64   `json:"bytes_received"`
	TotalBytes     int64   `json:"total_bytes"`
	AvgSessionTime float64 `json:"avg_session_time_seconds"`
	FailedConns    int     `json:"failed_connections"`
}

// Traffic by Protocol
type protocolTrafficStats struct {
	Protocol       string  `json:"protocol"`
	Connections    int     `json:"connections"`
	UniqueIPs      int     `json:"unique_ips"`
	BytesSent      int64   `json:"bytes_sent"`
	BytesReceived  int64   `json:"bytes_received"`
	TotalBytes     int64   `json:"total_bytes"`
	AvgSessionTime float64 `json:"avg_session_time_seconds"`
}

// Daily statistics
type dailyStats struct {
	Date          string `json:"date"`
	Connections   int    `json:"connections"`
	UniqueIPs     int    `json:"unique_ips"`
	BytesSent     int64  `json:"bytes_sent"`
	BytesReceived int64  `json:"bytes_received"`
	TotalBytes    int64  `json:"total_bytes"`
	FailedConns   int    `json:"failed_connections"`
}

// Hourly statistics
type hourlyStats struct {
	Hour          int   `json:"hour"`
	Connections   int   `json:"connections"`
	UniqueIPs     int   `json:"unique_ips"`
	BytesSent     int64 `json:"bytes_sent"`
	BytesReceived int64 `json:"bytes_received"`
	TotalBytes    int64 `json:"total_bytes"`
}

// Failed connections details
type failedConnection struct {
	ClientIP     string `json:"client_ip"`
	Country      string `json:"country,omitempty"`
	Timestamp    string `json:"timestamp"`
	Protocol     string `json:"protocol"`
	Status       int    `json:"status"`
	ProxyAddr    string `json:"proxy_addr"`
	UpstreamAddr string `json:"upstream_addr"`
}

// Top talkers (IPs with most traffic)
type topTalker struct {
	IP          string `json:"ip"`
	Country     string `json:"country,omitempty"`
	TotalBytes  int64  `json:"total_bytes"`
	Connections int    `json:"connections"`
}

// Upstream server statistics
type upstreamStats struct {
	UpstreamAddr   string  `json:"upstream_addr"`
	Connections    int     `json:"connections"`
	UniqueIPs      int     `json:"unique_ips"`
	BytesSent      int64   `json:"bytes_sent"`
	BytesReceived  int64   `json:"bytes_received"`
	TotalBytes     int64   `json:"total_bytes"`
	AvgSessionTime float64 `json:"avg_session_time_seconds"`
	FailedConns    int     `json:"failed_connections"`
}

// IP and Port combination statistics
type ipPortStats struct {
	IP            string `json:"ip"`
	Country       string `json:"country,omitempty"`
	Port          string `json:"port"`
	Connections   int    `json:"connections"`
	BytesSent     int64  `json:"bytes_sent"`
	BytesReceived int64  `json:"bytes_received"`
	TotalBytes    int64  `json:"total_bytes"`
}

// Country and Port combination statistics
type countryPortStats struct {
	Country       string `json:"country"`
	Port          string `json:"port"`
	Connections   int    `json:"connections"`
	UniqueIPs     int    `json:"unique_ips"`
	BytesSent     int64  `json:"bytes_sent"`
	BytesReceived int64  `json:"bytes_received"`
	TotalBytes    int64  `json:"total_bytes"`
}

// Country drill-down
type countryDetailResponse struct {
	Country         string            `json:"country"`
	Connections     int               `json:"connections"`
	UniqueIPs       int               `json:"unique_ips"`
	UniqueUpstreams int               `json:"unique_upstreams"`
	TotalBytes      int64             `json:"total_bytes"`
	AvgSession      float64           `json:"avg_session_time_seconds"`
	MedianSession   float64           `json:"median_session_time_seconds"`
	TopIPs          []countryIPDetail `json:"top_ips"`
}

type countryIPDetail struct {
	IP          string  `json:"ip"`
	Connections int     `json:"connections"`
	TotalBytes  int64   `json:"total_bytes"`
	AvgSession  float64 `json:"avg_session_seconds"`
}

type serverActivityResponse struct {
	Upstream string        `json:"upstream"`
	Daily    []dailyStats  `json:"daily"`
	Hourly   []hourlyStats `json:"hourly"`
}

// Timeline telemetry
type timelinePoint struct {
	Timestamp       string  `json:"timestamp"`
	Connections     int     `json:"connections"`
	Failed          int     `json:"failed"`
	FailureRate     float64 `json:"failure_rate"`
	TotalBytes      int64   `json:"total_bytes"`
	UniqueIPs       int     `json:"unique_ips"`
	UniqueUpstreams int     `json:"unique_upstreams"`
	AvgSession      float64 `json:"avg_session_seconds"`
}

type timelineAnomaly struct {
	Timestamp       string   `json:"timestamp"`
	Connections     int      `json:"connections"`
	Failed          int      `json:"failed"`
	FailureRate     float64  `json:"failure_rate"`
	TotalBytes      int64    `json:"total_bytes"`
	UniqueIPs       int      `json:"unique_ips"`
	UniqueUpstreams int      `json:"unique_upstreams"`
	Score           float64  `json:"score"`
	Signals         []string `json:"signals"`
}

type timelineBucket struct {
	Time            time.Time
	Connections     int
	Failed          int
	TotalBytes      int64
	SumSession      float64
	UniqueIPs       int
	UniqueUpstreams int
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

	// Try to open GeoIP database
	var geoIPDB *geoip2.Reader
	if dbPath, err := findGeoIPDatabase(); err == nil {
		if db, err := geoip2.Open(dbPath); err == nil {
			geoIPDB = db
			defer db.Close()
		}
	}

	// Add country information to entries
	for i := range entries {
		if geoIPDB != nil {
			entries[i].Country = getCountryFromIP(geoIPDB, entries[i].ClientIP)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(streamLogResponse{
		Count:   len(entries),
		Entries: entries,
	})
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

func parseDateFilters(r *http.Request) (time.Time, time.Time) {
	layout := "2006-01-02"
	var startTime, endTime time.Time

	if startStr := r.URL.Query().Get("start"); startStr != "" {
		if t, err := time.Parse(layout, startStr); err == nil {
			startTime = t
		}
	}

	if endStr := r.URL.Query().Get("end"); endStr != "" {
		if t, err := time.Parse(layout, endStr); err == nil {
			endTime = t.Add(24 * time.Hour)
		}
	}

	return startTime, endTime
}

func meanAndStd(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}

	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))

	varianceSum := 0.0
	for _, v := range values {
		diff := v - mean
		varianceSum += diff * diff
	}
	variance := varianceSum / float64(len(values))

	return mean, math.Sqrt(variance)
}

func filterEntriesByDate(entries []streamLogEntry, start, end time.Time) []streamLogEntry {
	if start.IsZero() && end.IsZero() {
		return entries
	}
	filtered := make([]streamLogEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Time.IsZero() {
			continue
		}
		if !start.IsZero() && entry.Time.Before(start) {
			continue
		}
		if !end.IsZero() && entry.Time.After(end) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func medianFloat64(values []float64) float64 {
	switch len(values) {
	case 0:
		return 0
	case 1:
		return values[0]
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}

func buildTimelineBuckets(entries []streamLogEntry, startTime, endTime time.Time) []timelineBucket {
	timelineMap := make(map[string]*timelineBucket)
	ipSets := make(map[string]map[string]struct{})
	upstreamSets := make(map[string]map[string]struct{})

	for _, entry := range entries {
		if entry.Time.IsZero() {
			continue
		}

		if !startTime.IsZero() && entry.Time.Before(startTime) {
			continue
		}
		if !endTime.IsZero() && entry.Time.After(endTime) {
			continue
		}

		bucketTime := entry.Time.UTC().Truncate(time.Hour)
		key := bucketTime.Format(time.RFC3339)

		bucket, exists := timelineMap[key]
		if !exists {
			bucket = &timelineBucket{Time: bucketTime}
			timelineMap[key] = bucket
			ipSets[key] = make(map[string]struct{})
			upstreamSets[key] = make(map[string]struct{})
		}

		bucket.Connections++
		if entry.Status != 200 {
			bucket.Failed++
		}
		bucket.TotalBytes += entry.BytesSent + entry.BytesReceived
		bucket.SumSession += entry.SessionTime

		if entry.ClientIP != "" {
			ipSets[key][entry.ClientIP] = struct{}{}
		}
		if entry.UpstreamAddr != "" {
			upstreamSets[key][entry.UpstreamAddr] = struct{}{}
		}
	}

	buckets := make([]timelineBucket, 0, len(timelineMap))
	for key, bucket := range timelineMap {
		bucket.UniqueIPs = len(ipSets[key])
		bucket.UniqueUpstreams = len(upstreamSets[key])
		buckets = append(buckets, *bucket)
	}

	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].Time.Before(buckets[j].Time)
	})

	return buckets
}

// findGeoIPDatabase looks for a GeoIP database file in the geoIPDirectory
func findGeoIPDatabase() (string, error) {
	if err := os.MkdirAll(geoIPDirectory, 0o755); err != nil {
		return "", err
	}

	// Try preferred databases first
	for _, name := range preferredGeoIPDB {
		candidate := filepath.Join(geoIPDirectory, name)
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			return candidate, nil
		}
	}

	// Fall back to any .mmdb file
	files, err := filepath.Glob(filepath.Join(geoIPDirectory, "*.mmdb"))
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", errGeoIPUnavailable
	}
	return files[0], nil
}

// getCountryFromIP returns the country name for a given IP address
func getCountryFromIP(db *geoip2.Reader, ipStr string) string {
	if db == nil {
		return ""
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ""
	}

	// Try City database first (it has more detailed info)
	if record, err := db.City(ip); err == nil && record.Country.Names["en"] != "" {
		return record.Country.Names["en"]
	}

	// Fall back to Country database
	if record, err := db.Country(ip); err == nil && record.Country.Names["en"] != "" {
		return record.Country.Names["en"]
	}

	return ""
}

func respondJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// ============================================================================
// ANALYTICS ENDPOINTS
// ============================================================================
// The following endpoints provide various analytics and statistics about
// stream proxy traffic. All endpoints return JSON data.
//
// Available endpoints:
// - GET /streamInfo/summary                  - Overall summary statistics
// - GET /streamInfo/traffic/by-ip            - Traffic grouped by IP
// - GET /streamInfo/traffic/by-country       - Traffic grouped by country
// - GET /streamInfo/traffic/by-port          - Traffic grouped by port
// - GET /streamInfo/traffic/by-protocol      - Traffic grouped by protocol
// - GET /streamInfo/traffic/by-ip-port       - Traffic by IP+Port combination
// - GET /streamInfo/traffic/by-country-port  - Traffic by Country+Port combo
// - GET /streamInfo/stats/daily              - Daily statistics
// - GET /streamInfo/stats/hourly             - Hourly statistics (0-23)
// - GET /streamInfo/top/talkers              - Top IPs by traffic volume
// - GET /streamInfo/top/countries            - Top countries by traffic
// - GET /streamInfo/top/ports                - Top ports by connections
// - GET /streamInfo/upstream                 - Upstream server statistics
// - GET /streamInfo/failed                   - Failed connection attempts
// ============================================================================

// Helper function to load entries with GeoIP data
func loadEntriesWithGeoIP() ([]streamLogEntry, *geoip2.Reader, error) {
	entries, err := readStreamLogEntries()
	if err != nil {
		return nil, nil, err
	}

	var geoIPDB *geoip2.Reader
	if dbPath, err := findGeoIPDatabase(); err == nil {
		if db, err := geoip2.Open(dbPath); err == nil {
			geoIPDB = db
		}
	}

	// Add country information to entries
	if geoIPDB != nil {
		for i := range entries {
			entries[i].Country = getCountryFromIP(geoIPDB, entries[i].ClientIP)
		}
	}

	return entries, geoIPDB, nil
}

// Helper function to extract port from upstream address
func extractPort(upstreamAddr string) string {
	parts := strings.Split(upstreamAddr, ":")
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return "unknown"
}

// getSummary returns overall summary statistics
func getSummary(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	uniqueIPs := make(map[string]bool)
	uniqueCountries := make(map[string]bool)
	uniqueUpstreams := make(map[string]bool)
	var totalBytesSent, totalBytesRecv int64
	var totalSessionTime float64
	successCount, failCount := 0, 0
	var minTime, maxTime time.Time

	for _, entry := range entries {
		uniqueIPs[entry.ClientIP] = true
		if entry.Country != "" {
			uniqueCountries[entry.Country] = true
		}
		uniqueUpstreams[entry.UpstreamAddr] = true
		totalBytesSent += entry.BytesSent
		totalBytesRecv += entry.BytesReceived
		totalSessionTime += entry.SessionTime

		if entry.Status == 200 {
			successCount++
		} else {
			failCount++
		}

		if !entry.Time.IsZero() {
			if minTime.IsZero() || entry.Time.Before(minTime) {
				minTime = entry.Time
			}
			if maxTime.IsZero() || entry.Time.After(maxTime) {
				maxTime = entry.Time
			}
		}
	}

	avgSessionTime := 0.0
	if len(entries) > 0 {
		avgSessionTime = totalSessionTime / float64(len(entries))
	}

	dateRange := "N/A"
	if !minTime.IsZero() && !maxTime.IsZero() {
		dateRange = minTime.Format("2006-01-02") + " to " + maxTime.Format("2006-01-02")
	}

	stats := summaryStats{
		TotalConnections: len(entries),
		UniqueIPs:        len(uniqueIPs),
		TotalBytesSent:   totalBytesSent,
		TotalBytesRecv:   totalBytesRecv,
		TotalBytes:       totalBytesSent + totalBytesRecv,
		AvgSessionTime:   avgSessionTime,
		SuccessfulConns:  successCount,
		FailedConns:      failCount,
		UniqueCountries:  len(uniqueCountries),
		UniqueUpstreams:  len(uniqueUpstreams),
		DateRange:        dateRange,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

// getTrafficByIP returns traffic statistics grouped by IP address
func getTrafficByIP(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	ipMap := make(map[string]*ipTrafficStats)

	for _, entry := range entries {
		stat, exists := ipMap[entry.ClientIP]
		if !exists {
			stat = &ipTrafficStats{
				IP:      entry.ClientIP,
				Country: entry.Country,
			}
			ipMap[entry.ClientIP] = stat
		}

		stat.Connections++
		stat.BytesSent += entry.BytesSent
		stat.BytesReceived += entry.BytesReceived
		stat.TotalBytes += entry.BytesSent + entry.BytesReceived
		stat.AvgSessionTime += entry.SessionTime

		if entry.Status != 200 {
			stat.FailedConns++
		}
	}

	// Calculate averages
	result := make([]ipTrafficStats, 0, len(ipMap))
	for _, stat := range ipMap {
		if stat.Connections > 0 {
			stat.AvgSessionTime /= float64(stat.Connections)
		}
		result = append(result, *stat)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// getTrafficByCountry returns traffic statistics grouped by country
func getTrafficByCountry(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	startTime, endTime := parseDateFilters(r)
	entries = filterEntriesByDate(entries, startTime, endTime)

	countryMap := make(map[string]*countryTrafficStats)
	countryIPMap := make(map[string]map[string]bool)

	for _, entry := range entries {
		country := entry.Country
		if country == "" {
			country = "Unknown"
		}

		stat, exists := countryMap[country]
		if !exists {
			stat = &countryTrafficStats{
				Country: country,
			}
			countryMap[country] = stat
			countryIPMap[country] = make(map[string]bool)
		}

		stat.Connections++
		stat.BytesSent += entry.BytesSent
		stat.BytesReceived += entry.BytesReceived
		stat.TotalBytes += entry.BytesSent + entry.BytesReceived
		stat.AvgSessionTime += entry.SessionTime
		countryIPMap[country][entry.ClientIP] = true
	}

	// Calculate averages and unique IPs
	result := make([]countryTrafficStats, 0, len(countryMap))
	for country, stat := range countryMap {
		if stat.Connections > 0 {
			stat.AvgSessionTime /= float64(stat.Connections)
		}
		stat.UniqueIPs = len(countryIPMap[country])
		result = append(result, *stat)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func getCountryDetail(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	countryParam := strings.TrimSpace(chi.URLParam(r, "country"))
	if countryParam == "" {
		respondJSONError(w, http.StatusBadRequest, "country parameter is required")
		return
	}

	startTime, endTime := parseDateFilters(r)
	entries = filterEntriesByDate(entries, startTime, endTime)

	target := strings.ToLower(countryParam)
	if target == "" {
		respondJSONError(w, http.StatusBadRequest, "country parameter is required")
		return
	}

	detail := countryDetailResponse{Country: countryParam}
	uniqueIPs := make(map[string]struct{})
	uniqueUpstreams := make(map[string]struct{})
	sessionValues := make([]float64, 0, len(entries))

	type ipAccumulator struct {
		IP          string
		Connections int
		TotalBytes  int64
		SessionSum  float64
	}

	ipStats := make(map[string]*ipAccumulator)

	var sessionSum float64
	displayName := ""

	for _, entry := range entries {
		countryName := entry.Country
		if countryName == "" {
			countryName = "Unknown"
		}

		if strings.ToLower(countryName) != target {
			continue
		}

		if displayName == "" {
			displayName = countryName
		}

		detail.Connections++
		bytes := entry.BytesSent + entry.BytesReceived
		detail.TotalBytes += bytes
		sessionSum += entry.SessionTime
		sessionValues = append(sessionValues, entry.SessionTime)

		if entry.ClientIP != "" {
			uniqueIPs[entry.ClientIP] = struct{}{}
		}
		if entry.UpstreamAddr != "" {
			uniqueUpstreams[entry.UpstreamAddr] = struct{}{}
		}

		if entry.ClientIP != "" {
			stat, exists := ipStats[entry.ClientIP]
			if !exists {
				stat = &ipAccumulator{IP: entry.ClientIP}
				ipStats[entry.ClientIP] = stat
			}
			stat.Connections++
			stat.TotalBytes += bytes
			stat.SessionSum += entry.SessionTime
		}
	}

	if detail.Connections == 0 {
		respondJSONError(w, http.StatusNotFound, "no telemetry for country")
		return
	}

	detail.Country = displayName
	detail.UniqueIPs = len(uniqueIPs)
	detail.UniqueUpstreams = len(uniqueUpstreams)
	detail.AvgSession = sessionSum / float64(detail.Connections)
	detail.MedianSession = medianFloat64(sessionValues)

	topIPs := make([]countryIPDetail, 0, len(ipStats))
	for _, stat := range ipStats {
		if stat.Connections == 0 {
			continue
		}
		topIPs = append(topIPs, countryIPDetail{
			IP:          stat.IP,
			Connections: stat.Connections,
			TotalBytes:  stat.TotalBytes,
			AvgSession:  stat.SessionSum / float64(stat.Connections),
		})
	}

	sort.Slice(topIPs, func(i, j int) bool {
		return topIPs[i].TotalBytes > topIPs[j].TotalBytes
	})
	if len(topIPs) > countryIPDetailLimit {
		topIPs = topIPs[:countryIPDetailLimit]
	}
	detail.TopIPs = topIPs

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(detail)
}

func getServerActivity(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	upstreamParam := strings.TrimSpace(chi.URLParam(r, "upstream"))
	if decoded, err := url.PathUnescape(upstreamParam); err == nil {
		upstreamParam = decoded
	}
	if upstreamParam == "" {
		respondJSONError(w, http.StatusBadRequest, "upstream parameter is required")
		return
	}

	startTime, endTime := parseDateFilters(r)
	entries = filterEntriesByDate(entries, startTime, endTime)

	target := upstreamParam
	dailyMap := make(map[string]*dailyStats)
	dailyIPMap := make(map[string]map[string]struct{})
	hourlyMap := make(map[int]*hourlyStats)
	hourlyIPMap := make(map[int]map[string]struct{})

	for i := 0; i < 24; i++ {
		hourlyMap[i] = &hourlyStats{Hour: i}
		hourlyIPMap[i] = make(map[string]struct{})
	}

	totalMatches := 0
	for _, entry := range entries {
		if entry.UpstreamAddr != target {
			continue
		}
		if entry.Time.IsZero() {
			continue
		}
		totalMatches++
		date := entry.Time.Format("2006-01-02")
		if _, ok := dailyMap[date]; !ok {
			dailyMap[date] = &dailyStats{Date: date}
			dailyIPMap[date] = make(map[string]struct{})
		}
		stat := dailyMap[date]
		stat.Connections++
		stat.BytesSent += entry.BytesSent
		stat.BytesReceived += entry.BytesReceived
		stat.TotalBytes += entry.BytesSent + entry.BytesReceived
		if entry.Status != 200 {
			stat.FailedConns++
		}
		if entry.ClientIP != "" {
			dailyIPMap[date][entry.ClientIP] = struct{}{}
		}

		hour := entry.Time.Hour()
		hStat := hourlyMap[hour]
		hStat.Connections++
		hStat.BytesSent += entry.BytesSent
		hStat.BytesReceived += entry.BytesReceived
		hStat.TotalBytes += entry.BytesSent + entry.BytesReceived
		if entry.ClientIP != "" {
			hourlyIPMap[hour][entry.ClientIP] = struct{}{}
		}
	}

	if totalMatches == 0 {
		respondJSONError(w, http.StatusNotFound, "no telemetry for upstream")
		return
	}

	daily := make([]dailyStats, 0, len(dailyMap))
	for date, stat := range dailyMap {
		stat.UniqueIPs = len(dailyIPMap[date])
		daily = append(daily, *stat)
	}
	sort.Slice(daily, func(i, j int) bool {
		return daily[i].Date < daily[j].Date
	})

	hourly := make([]hourlyStats, 24)
	for i := 0; i < 24; i++ {
		stat := hourlyMap[i]
		stat.UniqueIPs = len(hourlyIPMap[i])
		hourly[i] = *stat
	}

	resp := serverActivityResponse{
		Upstream: target,
		Daily:    daily,
		Hourly:   hourly,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// getTrafficByPort returns traffic statistics grouped by port
func getTrafficByPort(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	portMap := make(map[string]*portTrafficStats)
	portIPMap := make(map[string]map[string]bool)

	for _, entry := range entries {
		port := extractPort(entry.UpstreamAddr)

		stat, exists := portMap[port]
		if !exists {
			stat = &portTrafficStats{
				Port: port,
			}
			portMap[port] = stat
			portIPMap[port] = make(map[string]bool)
		}

		stat.Connections++
		stat.BytesSent += entry.BytesSent
		stat.BytesReceived += entry.BytesReceived
		stat.TotalBytes += entry.BytesSent + entry.BytesReceived
		stat.AvgSessionTime += entry.SessionTime
		portIPMap[port][entry.ClientIP] = true

		if entry.Status != 200 {
			stat.FailedConns++
		}
	}

	// Calculate averages and unique IPs
	result := make([]portTrafficStats, 0, len(portMap))
	for port, stat := range portMap {
		if stat.Connections > 0 {
			stat.AvgSessionTime /= float64(stat.Connections)
		}
		stat.UniqueIPs = len(portIPMap[port])
		result = append(result, *stat)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// getTrafficByProtocol returns traffic statistics grouped by protocol
func getTrafficByProtocol(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	protocolMap := make(map[string]*protocolTrafficStats)
	protocolIPMap := make(map[string]map[string]bool)

	for _, entry := range entries {
		protocol := entry.Protocol

		stat, exists := protocolMap[protocol]
		if !exists {
			stat = &protocolTrafficStats{
				Protocol: protocol,
			}
			protocolMap[protocol] = stat
			protocolIPMap[protocol] = make(map[string]bool)
		}

		stat.Connections++
		stat.BytesSent += entry.BytesSent
		stat.BytesReceived += entry.BytesReceived
		stat.TotalBytes += entry.BytesSent + entry.BytesReceived
		stat.AvgSessionTime += entry.SessionTime
		protocolIPMap[protocol][entry.ClientIP] = true
	}

	// Calculate averages and unique IPs
	result := make([]protocolTrafficStats, 0, len(protocolMap))
	for protocol, stat := range protocolMap {
		if stat.Connections > 0 {
			stat.AvgSessionTime /= float64(stat.Connections)
		}
		stat.UniqueIPs = len(protocolIPMap[protocol])
		result = append(result, *stat)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// getDailyStats returns daily statistics
func getDailyStats(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	// Parse query parameters for date filtering
	startDate := r.URL.Query().Get("start")
	endDate := r.URL.Query().Get("end")

	var startTime, endTime time.Time
	if startDate != "" {
		if t, err := time.Parse("2006-01-02", startDate); err == nil {
			startTime = t
		}
	}
	if endDate != "" {
		if t, err := time.Parse("2006-01-02", endDate); err == nil {
			endTime = t.Add(24 * time.Hour) // Include the entire end date
		}
	}

	dailyMap := make(map[string]*dailyStats)
	dailyIPMap := make(map[string]map[string]bool)

	for _, entry := range entries {
		if entry.Time.IsZero() {
			continue
		}

		// Apply date filter
		if !startTime.IsZero() && entry.Time.Before(startTime) {
			continue
		}
		if !endTime.IsZero() && entry.Time.After(endTime) {
			continue
		}

		date := entry.Time.Format("2006-01-02")

		stat, exists := dailyMap[date]
		if !exists {
			stat = &dailyStats{
				Date: date,
			}
			dailyMap[date] = stat
			dailyIPMap[date] = make(map[string]bool)
		}

		stat.Connections++
		stat.BytesSent += entry.BytesSent
		stat.BytesReceived += entry.BytesReceived
		stat.TotalBytes += entry.BytesSent + entry.BytesReceived
		dailyIPMap[date][entry.ClientIP] = true

		if entry.Status != 200 {
			stat.FailedConns++
		}
	}

	// Calculate unique IPs per day
	result := make([]dailyStats, 0, len(dailyMap))
	for date, stat := range dailyMap {
		stat.UniqueIPs = len(dailyIPMap[date])
		result = append(result, *stat)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// getHourlyStats returns hourly statistics (24 hour breakdown)
func getHourlyStats(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	// Parse query parameters for date filtering
	startDate := r.URL.Query().Get("start")
	endDate := r.URL.Query().Get("end")

	var startTime, endTime time.Time
	if startDate != "" {
		if t, err := time.Parse("2006-01-02", startDate); err == nil {
			startTime = t
		}
	}
	if endDate != "" {
		if t, err := time.Parse("2006-01-02", endDate); err == nil {
			endTime = t.Add(24 * time.Hour) // Include the entire end date
		}
	}

	hourlyMap := make(map[int]*hourlyStats)
	hourlyIPMap := make(map[int]map[string]bool)

	// Initialize all hours
	for i := 0; i < 24; i++ {
		hourlyMap[i] = &hourlyStats{Hour: i}
		hourlyIPMap[i] = make(map[string]bool)
	}

	for _, entry := range entries {
		if entry.Time.IsZero() {
			continue
		}

		// Apply date filter
		if !startTime.IsZero() && entry.Time.Before(startTime) {
			continue
		}
		if !endTime.IsZero() && entry.Time.After(endTime) {
			continue
		}

		hour := entry.Time.Hour()
		stat := hourlyMap[hour]

		stat.Connections++
		stat.BytesSent += entry.BytesSent
		stat.BytesReceived += entry.BytesReceived
		stat.TotalBytes += entry.BytesSent + entry.BytesReceived
		hourlyIPMap[hour][entry.ClientIP] = true
	}

	// Calculate unique IPs per hour
	result := make([]hourlyStats, 24)
	for i := 0; i < 24; i++ {
		stat := hourlyMap[i]
		stat.UniqueIPs = len(hourlyIPMap[i])
		result[i] = *stat
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// getTimeline returns chronological hourly buckets with richer telemetry
func getTimeline(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	startTime, endTime := parseDateFilters(r)
	buckets := buildTimelineBuckets(entries, startTime, endTime)

	points := make([]timelinePoint, len(buckets))
	for i, bucket := range buckets {
		avgSession := 0.0
		if bucket.Connections > 0 {
			avgSession = bucket.SumSession / float64(bucket.Connections)
		}

		failureRate := 0.0
		if bucket.Connections > 0 {
			failureRate = float64(bucket.Failed) / float64(bucket.Connections)
		}

		points[i] = timelinePoint{
			Timestamp:       bucket.Time.Format(time.RFC3339),
			Connections:     bucket.Connections,
			Failed:          bucket.Failed,
			FailureRate:     failureRate,
			TotalBytes:      bucket.TotalBytes,
			UniqueIPs:       bucket.UniqueIPs,
			UniqueUpstreams: bucket.UniqueUpstreams,
			AvgSession:      avgSession,
		}
	}

	if len(points) > 240 {
		points = points[len(points)-240:]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(points)
}

// getTimelineAnomalies inspects timeline buckets for unusual activity
func getTimelineAnomalies(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	startTime, endTime := parseDateFilters(r)
	buckets := buildTimelineBuckets(entries, startTime, endTime)

	if len(buckets) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]timelineAnomaly{})
		return
	}

	connValues := make([]float64, len(buckets))
	failureValues := make([]float64, len(buckets))
	byteValues := make([]float64, len(buckets))

	for i, bucket := range buckets {
		connValues[i] = float64(bucket.Connections)
		failureRate := 0.0
		if bucket.Connections > 0 {
			failureRate = float64(bucket.Failed) / float64(bucket.Connections)
		}
		failureValues[i] = failureRate
		byteValues[i] = float64(bucket.TotalBytes)
	}

	meanConn, stdConn := meanAndStd(connValues)
	meanFail, stdFail := meanAndStd(failureValues)
	meanBytes, stdBytes := meanAndStd(byteValues)

	anomalies := make([]timelineAnomaly, 0)

	for _, bucket := range buckets {
		if bucket.Connections == 0 {
			continue
		}

		failureRate := float64(bucket.Failed) / float64(bucket.Connections)
		var signals []string
		score := 0.0

		if stdConn > 0 {
			z := (float64(bucket.Connections) - meanConn) / stdConn
			if math.Abs(z) >= 2 {
				direction := "spike"
				if z < 0 {
					direction = "drop"
				}
				signals = append(signals, fmt.Sprintf("Connection %s (z=%.2f)", direction, z))
				score = math.Max(score, math.Abs(z))
			}
		}

		failThreshold := meanFail + math.Max(0.05, stdFail)
		if failureRate > failThreshold && bucket.Connections >= 10 {
			signals = append(signals, fmt.Sprintf("Failure rate %.1f%% (baseline %.1f%%)", failureRate*100, meanFail*100))
			score = math.Max(score, (failureRate-failThreshold)*20)
		}

		if stdBytes > 0 {
			z := (float64(bucket.TotalBytes) - meanBytes) / stdBytes
			if math.Abs(z) >= 2.5 {
				direction := "surge"
				if z < 0 {
					direction = "drop"
				}
				signals = append(signals, fmt.Sprintf("Traffic %s (z=%.2f)", direction, z))
				score = math.Max(score, math.Abs(z))
			}
		}

		if len(signals) == 0 {
			continue
		}

		anomalies = append(anomalies, timelineAnomaly{
			Timestamp:       bucket.Time.Format(time.RFC3339),
			Connections:     bucket.Connections,
			Failed:          bucket.Failed,
			FailureRate:     failureRate,
			TotalBytes:      bucket.TotalBytes,
			UniqueIPs:       bucket.UniqueIPs,
			UniqueUpstreams: bucket.UniqueUpstreams,
			Score:           score,
			Signals:         signals,
		})
	}

	sort.Slice(anomalies, func(i, j int) bool {
		if anomalies[i].Score == anomalies[j].Score {
			return anomalies[i].Timestamp > anomalies[j].Timestamp
		}
		return anomalies[i].Score > anomalies[j].Score
	})

	if len(anomalies) > 12 {
		anomalies = anomalies[:12]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(anomalies)
}

// getFailedConnections returns all failed connection attempts
func getFailedConnections(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	failed := make([]failedConnection, 0)

	for _, entry := range entries {
		if entry.Status != 200 {
			failed = append(failed, failedConnection{
				ClientIP:     entry.ClientIP,
				Country:      entry.Country,
				Timestamp:    entry.Timestamp,
				Protocol:     entry.Protocol,
				Status:       entry.Status,
				ProxyAddr:    entry.ProxyAddr,
				UpstreamAddr: entry.UpstreamAddr,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(failed)
}

// getTopTalkers returns top N IPs by total traffic
func getTopTalkers(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	ipMap := make(map[string]*topTalker)

	for _, entry := range entries {
		talker, exists := ipMap[entry.ClientIP]
		if !exists {
			talker = &topTalker{
				IP:      entry.ClientIP,
				Country: entry.Country,
			}
			ipMap[entry.ClientIP] = talker
		}

		talker.TotalBytes += entry.BytesSent + entry.BytesReceived
		talker.Connections++
	}

	// Convert to slice and sort by total bytes
	result := make([]topTalker, 0, len(ipMap))
	for _, talker := range ipMap {
		result = append(result, *talker)
	}

	// Sort by total bytes descending
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].TotalBytes > result[i].TotalBytes {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	// Limit to top N
	limit := 10
	if len(result) > limit {
		result = result[:limit]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// getUpstreamStats returns statistics for upstream servers
func getUpstreamStats(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	upstreamMap := make(map[string]*upstreamStats)
	upstreamIPMap := make(map[string]map[string]bool)

	for _, entry := range entries {
		upstream := entry.UpstreamAddr

		stat, exists := upstreamMap[upstream]
		if !exists {
			stat = &upstreamStats{
				UpstreamAddr: upstream,
			}
			upstreamMap[upstream] = stat
			upstreamIPMap[upstream] = make(map[string]bool)
		}

		stat.Connections++
		stat.BytesSent += entry.BytesSent
		stat.BytesReceived += entry.BytesReceived
		stat.TotalBytes += entry.BytesSent + entry.BytesReceived
		stat.AvgSessionTime += entry.SessionTime
		upstreamIPMap[upstream][entry.ClientIP] = true

		if entry.Status != 200 {
			stat.FailedConns++
		}
	}

	// Calculate averages and unique IPs
	result := make([]upstreamStats, 0, len(upstreamMap))
	for upstream, stat := range upstreamMap {
		if stat.Connections > 0 {
			stat.AvgSessionTime /= float64(stat.Connections)
		}
		stat.UniqueIPs = len(upstreamIPMap[upstream])
		result = append(result, *stat)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// getTrafficByIPAndPort returns traffic statistics grouped by IP and Port combination
func getTrafficByIPAndPort(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	type ipPortKey struct {
		ip   string
		port string
	}

	ipPortMap := make(map[ipPortKey]*ipPortStats)

	for _, entry := range entries {
		port := extractPort(entry.UpstreamAddr)
		key := ipPortKey{ip: entry.ClientIP, port: port}

		stat, exists := ipPortMap[key]
		if !exists {
			stat = &ipPortStats{
				IP:      entry.ClientIP,
				Country: entry.Country,
				Port:    port,
			}
			ipPortMap[key] = stat
		}

		stat.Connections++
		stat.BytesSent += entry.BytesSent
		stat.BytesReceived += entry.BytesReceived
		stat.TotalBytes += entry.BytesSent + entry.BytesReceived
	}

	result := make([]ipPortStats, 0, len(ipPortMap))
	for _, stat := range ipPortMap {
		result = append(result, *stat)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// getTrafficByCountryAndPort returns traffic statistics grouped by Country and Port
func getTrafficByCountryAndPort(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	type countryPortKey struct {
		country string
		port    string
	}

	countryPortMap := make(map[countryPortKey]*countryPortStats)
	countryPortIPMap := make(map[countryPortKey]map[string]bool)

	for _, entry := range entries {
		country := entry.Country
		if country == "" {
			country = "Unknown"
		}
		port := extractPort(entry.UpstreamAddr)
		key := countryPortKey{country: country, port: port}

		stat, exists := countryPortMap[key]
		if !exists {
			stat = &countryPortStats{
				Country: country,
				Port:    port,
			}
			countryPortMap[key] = stat
			countryPortIPMap[key] = make(map[string]bool)
		}

		stat.Connections++
		stat.BytesSent += entry.BytesSent
		stat.BytesReceived += entry.BytesReceived
		stat.TotalBytes += entry.BytesSent + entry.BytesReceived
		countryPortIPMap[key][entry.ClientIP] = true
	}

	// Calculate unique IPs
	result := make([]countryPortStats, 0, len(countryPortMap))
	for key, stat := range countryPortMap {
		stat.UniqueIPs = len(countryPortIPMap[key])
		result = append(result, *stat)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// getTopCountries returns top N countries by total traffic
func getTopCountries(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	countryMap := make(map[string]*countryTrafficStats)
	countryIPMap := make(map[string]map[string]bool)

	for _, entry := range entries {
		country := entry.Country
		if country == "" {
			country = "Unknown"
		}

		stat, exists := countryMap[country]
		if !exists {
			stat = &countryTrafficStats{
				Country: country,
			}
			countryMap[country] = stat
			countryIPMap[country] = make(map[string]bool)
		}

		stat.Connections++
		stat.BytesSent += entry.BytesSent
		stat.BytesReceived += entry.BytesReceived
		stat.TotalBytes += entry.BytesSent + entry.BytesReceived
		stat.AvgSessionTime += entry.SessionTime
		countryIPMap[country][entry.ClientIP] = true
	}

	// Calculate averages and unique IPs
	result := make([]countryTrafficStats, 0, len(countryMap))
	for country, stat := range countryMap {
		if stat.Connections > 0 {
			stat.AvgSessionTime /= float64(stat.Connections)
		}
		stat.UniqueIPs = len(countryIPMap[country])
		result = append(result, *stat)
	}

	// Sort by total bytes descending
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].TotalBytes > result[i].TotalBytes {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	// Limit to top N
	limit := 10
	if len(result) > limit {
		result = result[:limit]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// getTopPorts returns top N ports by total connections
func getTopPorts(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	portMap := make(map[string]*portTrafficStats)
	portIPMap := make(map[string]map[string]bool)

	for _, entry := range entries {
		port := extractPort(entry.UpstreamAddr)

		stat, exists := portMap[port]
		if !exists {
			stat = &portTrafficStats{
				Port: port,
			}
			portMap[port] = stat
			portIPMap[port] = make(map[string]bool)
		}

		stat.Connections++
		stat.BytesSent += entry.BytesSent
		stat.BytesReceived += entry.BytesReceived
		stat.TotalBytes += entry.BytesSent + entry.BytesReceived
		stat.AvgSessionTime += entry.SessionTime
		portIPMap[port][entry.ClientIP] = true

		if entry.Status != 200 {
			stat.FailedConns++
		}
	}

	// Calculate averages and unique IPs
	result := make([]portTrafficStats, 0, len(portMap))
	for port, stat := range portMap {
		if stat.Connections > 0 {
			stat.AvgSessionTime /= float64(stat.Connections)
		}
		stat.UniqueIPs = len(portIPMap[port])
		result = append(result, *stat)
	}

	// Sort by connections descending
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Connections > result[i].Connections {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	// Limit to top N
	limit := 10
	if len(result) > limit {
		result = result[:limit]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// ConnectionFlow represents the flow from source to destination
type connectionFlow struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Connections int    `json:"connections"`
	TotalBytes  int64  `json:"total_bytes"`
}

// getConnectionFlow returns connection flow data for visualization (Country -> Port)
func getConnectionFlow(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	// Parse query parameters for date filtering
	startDate := r.URL.Query().Get("start")
	endDate := r.URL.Query().Get("end")
	limitStr := r.URL.Query().Get("limit")

	var startTime, endTime time.Time
	if startDate != "" {
		if t, err := time.Parse("2006-01-02", startDate); err == nil {
			startTime = t
		}
	}
	if endDate != "" {
		if t, err := time.Parse("2006-01-02", endDate); err == nil {
			endTime = t.Add(24 * time.Hour) // Include the entire end date
		}
	}

	limit := 100 // Default limit
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	type flowKey struct {
		source string
		dest   string
	}

	flowMap := make(map[flowKey]*connectionFlow)

	for _, entry := range entries {
		// Apply date filter
		if !startTime.IsZero() && entry.Time.Before(startTime) {
			continue
		}
		if !endTime.IsZero() && entry.Time.After(endTime) {
			continue
		}

		source := entry.Country
		if source == "" {
			source = "Unknown"
		}

		port := extractPort(entry.UpstreamAddr)
		dest := "Port " + port

		key := flowKey{source: source, dest: dest}

		flow, exists := flowMap[key]
		if !exists {
			flow = &connectionFlow{
				Source:      source,
				Destination: dest,
			}
			flowMap[key] = flow
		}

		flow.Connections++
		flow.TotalBytes += entry.BytesSent + entry.BytesReceived
	}

	// Convert to slice
	result := make([]connectionFlow, 0, len(flowMap))
	for _, flow := range flowMap {
		result = append(result, *flow)
	}

	// Sort by connections descending
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].Connections > result[i].Connections {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	// Apply limit
	if len(result) > limit {
		result = result[:limit]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// DateRange info for the dataset
type dateRangeInfo struct {
	MinDate      string `json:"min_date"`
	MaxDate      string `json:"max_date"`
	TotalDays    int    `json:"total_days"`
	TotalEntries int    `json:"total_entries"`
	AvgPerDay    int    `json:"avg_per_day"`
}

// getDateRange returns the date range of available data
func getDateRange(w http.ResponseWriter, r *http.Request) {
	entries, geoIPDB, err := loadEntriesWithGeoIP()
	if geoIPDB != nil {
		defer geoIPDB.Close()
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load entries")
		return
	}

	if len(entries) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(dateRangeInfo{})
		return
	}

	var minTime, maxTime time.Time
	for _, entry := range entries {
		if entry.Time.IsZero() {
			continue
		}
		if minTime.IsZero() || entry.Time.Before(minTime) {
			minTime = entry.Time
		}
		if maxTime.IsZero() || entry.Time.After(maxTime) {
			maxTime = entry.Time
		}
	}

	totalDays := 1
	if !minTime.IsZero() && !maxTime.IsZero() {
		totalDays = int(maxTime.Sub(minTime).Hours()/24) + 1
	}

	avgPerDay := 0
	if totalDays > 0 {
		avgPerDay = len(entries) / totalDays
	}

	info := dateRangeInfo{
		MinDate:      minTime.Format("2006-01-02"),
		MaxDate:      maxTime.Format("2006-01-02"),
		TotalDays:    totalDays,
		TotalEntries: len(entries),
		AvgPerDay:    avgPerDay,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}

func setupStreamInfo(r chi.Router) chi.Router {
	return r.Route("/streamInfo", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "static/streamInfo.html")
		})

		// Raw data
		r.Get("/data", getData)

		// Summary and overview
		r.Get("/summary", getSummary)

		// Traffic by different dimensions
		r.Get("/traffic/by-ip", getTrafficByIP)
		r.Get("/traffic/by-country", getTrafficByCountry)
		r.Get("/traffic/by-port", getTrafficByPort)
		r.Get("/traffic/by-protocol", getTrafficByProtocol)
		r.Get("/traffic/by-ip-port", getTrafficByIPAndPort)
		r.Get("/traffic/by-country-port", getTrafficByCountryAndPort)
		r.Get("/country/{country}", getCountryDetail)
		r.Get("/server/{upstream}/activity", getServerActivity)

		// Time-based statistics
		r.Get("/stats/daily", getDailyStats)
		r.Get("/stats/hourly", getHourlyStats)
		r.Get("/timeline", getTimeline)

		// Top lists
		r.Get("/top/talkers", getTopTalkers)
		r.Get("/top/countries", getTopCountries)
		r.Get("/top/ports", getTopPorts)

		// Upstream and failed connections
		r.Get("/upstream", getUpstreamStats)
		r.Get("/failed", getFailedConnections)

		// Advanced analytics
		r.Get("/flow", getConnectionFlow)
		r.Get("/daterange", getDateRange)
		r.Get("/anomalies", getTimelineAnomalies)
	})
}
