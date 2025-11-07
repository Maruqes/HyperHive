package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

	dailyMap := make(map[string]*dailyStats)
	dailyIPMap := make(map[string]map[string]bool)

	for _, entry := range entries {
		if entry.Time.IsZero() {
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

		// Time-based statistics
		r.Get("/stats/daily", getDailyStats)
		r.Get("/stats/hourly", getHourlyStats)

		// Top lists
		r.Get("/top/talkers", getTopTalkers)
		r.Get("/top/countries", getTopCountries)
		r.Get("/top/ports", getTopPorts)

		// Upstream and failed connections
		r.Get("/upstream", getUpstreamStats)
		r.Get("/failed", getFailedConnections)
	})
}
