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

func setupStreamInfo(r chi.Router) chi.Router {
	return r.Route("/streamInfo", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "static/streamInfo.html")
		})
		r.Get("/data", getData)
	})
}
