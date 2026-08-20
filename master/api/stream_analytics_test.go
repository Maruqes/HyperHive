package api

import (
	"512SvMan/dnsmasq"
	"512SvMan/npm"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/oschwald/geoip2-golang"
)

func TestParseAnalyticsEndpoint(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		ip   string
		port string
	}{
		{name: "IPv4 with port", raw: "192.168.76.165:25565", ip: "192.168.76.165", port: "25565"},
		{name: "bracketed IPv6 with port", raw: "[2001:db8::1]:443", ip: "2001:db8::1", port: "443"},
		{name: "bare IPv6", raw: "2001:db8::1", ip: "2001:db8::1", port: ""},
		{name: "IPv4 without port", raw: "10.0.0.1", ip: "10.0.0.1", port: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parseAnalyticsEndpoint(test.raw)
			if got.ip != test.ip || got.port != test.port {
				t.Fatalf("parseAnalyticsEndpoint(%q) = ip %q, port %q; want ip %q, port %q", test.raw, got.ip, got.port, test.ip, test.port)
			}
		})
	}
}

func TestParseAnalyticsListener(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		ip   string
		port string
	}{
		{name: "unbracketed IPv6", raw: "2001:db8::1:443", ip: "2001:db8::1", port: "443"},
		{name: "IPv4", raw: "192.168.1.10:25565", ip: "192.168.1.10", port: "25565"},
		{name: "bracketed IPv6", raw: "[2001:db8::2]:8443", ip: "2001:db8::2", port: "8443"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parseAnalyticsListener(test.raw)
			if got.ip != test.ip || got.port != test.port {
				t.Fatalf("parseAnalyticsListener(%q) = ip %q, port %q; want ip %q, port %q", test.raw, got.ip, got.port, test.ip, test.port)
			}
		})
	}

	generic := parseAnalyticsEndpoint("2001:db8::1:443")
	if generic.ip != "2001:db8::1:443" || generic.port != "" {
		t.Fatalf("generic bare IPv6 was treated as host+port: %#v", generic)
	}
	for _, invalid := range []string{"2001:db8::1:0", "2001:db8::1:65536", "[2001:db8::1]:0", "192.168.1.10:not-a-port"} {
		if got := parseAnalyticsListener(invalid); got.port != "" {
			t.Errorf("invalid listener %q produced port %q", invalid, got.port)
		}
	}
}

func TestAnalyticsAliasFormattingIsSortedAndUnique(t *testing.T) {
	aliases := buildAnalyticsAliasMap([]dnsmasq.AliasEntry{
		{Alias: "zeta.local", IP: "192.168.76.165"},
		{Alias: "alpha.local", IP: "192.168.76.165"},
		{Alias: "zeta.local", IP: "192.168.76.165"},
		{Alias: "ignored.local", IP: "not-an-ip"},
	})

	endpoint := enrichAnalyticsEndpoint("192.168.76.165:25565", aliases)
	wantAliases := []string{"alpha.local", "zeta.local"}
	if !reflect.DeepEqual(endpoint.Aliases, wantAliases) {
		t.Fatalf("aliases = %#v; want %#v", endpoint.Aliases, wantAliases)
	}
	if endpoint.Display != "alpha.local, zeta.local (192.168.76.165):25565" {
		t.Fatalf("display = %q", endpoint.Display)
	}
	if endpoint.Port != "25565" || endpoint.RawAddress != "192.168.76.165:25565" {
		t.Fatalf("port/raw were not preserved: %#v", endpoint)
	}
}

func TestAnalyticsIPScope(t *testing.T) {
	tests := map[string]string{
		"192.168.1.1": "private",
		"8.8.8.8":     "public",
		"127.0.0.1":   "loopback",
		"fe80::1":     "link-local",
		"ff02::1":     "multicast",
		"239.1.1.1":   "multicast",
		"0.0.0.0":     "unknown",
		"invalid":     "unknown",
	}
	for ip, want := range tests {
		if got := analyticsIPScope(ip); got != want {
			t.Errorf("analyticsIPScope(%q) = %q; want %q", ip, got, want)
		}
	}
}

func TestParseAnalyticsFiltersValidationAndLimits(t *testing.T) {
	tests := []struct {
		query string
		want  string
	}{
		{query: "start=2026-13-01", want: "start must use YYYY-MM-DD"},
		{query: "end=20-01-01", want: "end must use YYYY-MM-DD"},
		{query: "start=2026-08-21&end=2026-08-20", want: "start must not be after end"},
		{query: "outcome=maybe", want: "outcome must be success or failed"},
		{query: "path_limit=0", want: "path_limit must be a positive integer"},
		{query: "session_limit=nope", want: "session_limit must be a positive integer"},
		{query: "source_limit=0", want: "source_limit must be a positive integer"},
		{query: "source_limit=nope", want: "source_limit must be a positive integer"},
		{query: "source_cursor=not*a*cursor", want: "source_cursor must be a valid cursor"},
	}
	for _, test := range tests {
		req := httptest.NewRequest(http.MethodGet, "/streamInfo/analytics?"+test.query, nil)
		_, err := parseAnalyticsFilters(req)
		if err == nil || err.Error() != test.want {
			t.Errorf("query %q error = %v; want %q", test.query, err, test.want)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/streamInfo/analytics", nil)
	filters, err := parseAnalyticsFilters(req)
	if err != nil {
		t.Fatal(err)
	}
	if filters.sourceLimit != analyticsDefaultSourceLimit || filters.sourceCursor != "" || filters.sourceAfter != "" {
		t.Fatalf("default source page = %#v", filters)
	}

	cursor := encodeAnalyticsSourceCursor("2001:db8::2a")
	req = httptest.NewRequest(http.MethodGet, "/streamInfo/analytics?path_limit=99999&session_limit=99999&source_limit=99999&source_cursor="+cursor, nil)
	filters, err = parseAnalyticsFilters(req)
	if err != nil {
		t.Fatal(err)
	}
	if filters.pathLimit != analyticsMaxPathLimit || filters.sessionLimit != analyticsMaxSessionLimit {
		t.Fatalf("limits = %d/%d; want %d/%d", filters.pathLimit, filters.sessionLimit, analyticsMaxPathLimit, analyticsMaxSessionLimit)
	}
	if filters.sourceLimit != analyticsMaxSourceLimit || filters.sourceCursor != cursor || filters.sourceAfter != "2001:db8::2a" {
		t.Fatalf("source page = %#v", filters)
	}

	oversized := strings.Repeat("a", analyticsMaxSourceCursorSize+1)
	req = httptest.NewRequest(http.MethodGet, "/streamInfo/analytics?source_cursor="+oversized, nil)
	if _, err := parseAnalyticsFilters(req); err == nil || err.Error() != "source_cursor must be a valid cursor" {
		t.Fatalf("oversized cursor error = %v", err)
	}
}

func TestParseAnalyticsExactFilters(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/streamInfo/analytics?path_id=path_0123456789abcdef&source_ip=2001:0db8:0:0::1&listener_ip=192.168.001.1&destination_ip=2001:db8::2", nil)
	filters, err := parseAnalyticsFilters(req)
	if err == nil {
		t.Fatal("non-canonical IPv4 with leading zeroes must be rejected")
	}

	req = httptest.NewRequest(http.MethodGet, "/streamInfo/analytics?path_id=path_0123456789abcdef&source_ip=2001:0db8:0:0::1&listener_ip=192.168.1.1&destination_ip=2001:0db8::2", nil)
	filters, err = parseAnalyticsFilters(req)
	if err != nil {
		t.Fatal(err)
	}
	if filters.pathID != "path_0123456789abcdef" || filters.sourceIP != "2001:db8::1" || filters.listenerIP != "192.168.1.1" || filters.destinationIP != "2001:db8::2" {
		t.Fatalf("exact filters were not canonicalized: %#v", filters)
	}

	invalid := map[string]string{
		"path_id=path_0123456789abcde":  "path_id must use path_ followed by 16 lowercase hexadecimal characters",
		"path_id=path_0123456789abcdeF": "path_id must use path_ followed by 16 lowercase hexadecimal characters",
		"source_ip=not-an-ip":           "source_ip must be a valid IP address",
		"listener_ip=10.0.0.999":        "listener_ip must be a valid IP address",
		"destination_ip=host.example":   "destination_ip must be a valid IP address",
	}
	for query, want := range invalid {
		req := httptest.NewRequest(http.MethodGet, "/streamInfo/analytics?"+query, nil)
		_, err := parseAnalyticsFilters(req)
		if err == nil || err.Error() != want {
			t.Errorf("query %q error = %v; want %q", query, err, want)
		}
	}
}

func TestFilterAnalyticsEntriesComposesSearches(t *testing.T) {
	dayOne := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dayTwo := dayOne.Add(24 * time.Hour)
	entries := []streamLogEntry{
		{ClientIP: "10.0.0.2", ProxyAddr: "192.168.1.10:443", UpstreamAddr: "192.168.76.165:25565", Protocol: "TCP", Country: "Portugal", Status: 200, Time: dayOne},
		{ClientIP: "10.0.0.2", ProxyAddr: "192.168.1.10:443", UpstreamAddr: "192.168.76.165:25565", Protocol: "TCP", Country: "Portugal", Status: 502, Time: dayOne},
		{ClientIP: "10.0.0.3", ProxyAddr: "192.168.1.10:53", UpstreamAddr: "8.8.8.8:53", Protocol: "UDP", Country: "United States", Status: 200, Time: dayTwo},
	}
	aliases := buildAnalyticsAliasMap([]dnsmasq.AliasEntry{
		{Alias: "client.local", IP: "10.0.0.2"},
		{Alias: "vm.minelive", IP: "192.168.76.165"},
	})
	filters := analyticsFilters{
		start:        dayOne.Truncate(24 * time.Hour),
		endExclusive: dayOne.Truncate(24*time.Hour).AddDate(0, 0, 1),
		source:       "CLIENT",
		listener:     "1.10:44",
		destination:  "MINE",
		protocol:     "c",
		country:      "TUG",
		outcome:      "failed",
	}

	got := filterAnalyticsEntries(entries, filters, aliases)
	if len(got) != 1 || got[0].Status != 502 {
		t.Fatalf("filtered entries = %#v", got)
	}
}

func TestFilterAnalyticsEntriesExactIPs(t *testing.T) {
	entries := []streamLogEntry{
		{ClientIP: "10.0.0.2", ProxyAddr: "[2001:db8::1]:443", UpstreamAddr: "192.168.1.20:80"},
		{ClientIP: "10.0.0.20", ProxyAddr: "[2001:db8::1]:443", UpstreamAddr: "192.168.1.20:80"},
		{ClientIP: "10.0.0.2", ProxyAddr: "[2001:db8::10]:443", UpstreamAddr: "192.168.1.20:80"},
		{ClientIP: "10.0.0.2", ProxyAddr: "[2001:db8::1]:443", UpstreamAddr: "192.168.1.200:80"},
	}
	filters := analyticsFilters{sourceIP: "10.0.0.2", listenerIP: "2001:db8::1", destinationIP: "192.168.1.20"}
	got := filterAnalyticsEntries(entries, filters, map[string][]string{})
	if len(got) != 1 || got[0].ClientIP != "10.0.0.2" || got[0].ProxyAddr != "[2001:db8::1]:443" || got[0].UpstreamAddr != "192.168.1.20:80" {
		t.Fatalf("exact IP filters matched substring collisions: %#v", got)
	}
}

func TestAnalyticsPathIDStableAndFilterable(t *testing.T) {
	entry := streamLogEntry{
		ClientIP: "10.0.0.2", ProxyAddr: "2001:db8::1:25565", UpstreamAddr: "192.168.76.165:25565",
		Protocol: "TCP", Country: "Portugal", Status: http.StatusOK,
	}
	const pathID = "path_145235a4b54524f8"
	if got := analyticsPathID(analyticsPathKey(entry)); got != pathID {
		t.Fatalf("path ID changed: got %q, want %q", got, pathID)
	}
	otherCountry := entry
	otherCountry.Country = "United States"
	if got := analyticsPathID(analyticsPathKey(otherCountry)); got != pathID {
		t.Fatalf("country changed path ID: got %q, want %q", got, pathID)
	}

	entries := []streamLogEntry{entry, otherCountry}
	response := aggregateStreamAnalytics(entries, map[string][]string{}, []npm.Stream{}, map[int]string{}, true, analyticsFilters{pathLimit: 10, sessionLimit: 10})
	if len(response.Paths) != 1 || response.Paths[0].ID != pathID || response.Paths[0].Connections != 2 {
		t.Fatalf("aggregated paths = %#v", response.Paths)
	}
	matched := filterAnalyticsEntries(entries, analyticsFilters{pathID: pathID}, map[string][]string{})
	if len(matched) != 2 {
		t.Fatalf("known path ID returned %#v", matched)
	}
	unknown := filterAnalyticsEntries(entries, analyticsFilters{pathID: "path_0000000000000000"}, map[string][]string{})
	if len(unknown) != 0 {
		t.Fatalf("unknown valid path ID returned %#v", unknown)
	}
}

func TestAnalyticsEndpointSearchesAllFields(t *testing.T) {
	aliases := buildAnalyticsAliasMap([]dnsmasq.AliasEntry{{Alias: "vm.minelive", IP: "192.168.76.165"}})
	endpoint := enrichAnalyticsEndpoint("192.168.76.165:25565", aliases)
	for _, search := range []string{"192.168.76", "MINELIVE", "2556", "Vm.MineLive (192"} {
		if !analyticsEndpointMatches(endpoint, search) {
			t.Errorf("search %q did not match %#v", search, endpoint)
		}
	}
	if analyticsEndpointMatches(endpoint, "not-present") {
		t.Fatal("unexpected endpoint search match")
	}
}

func TestAggregateStreamAnalytics(t *testing.T) {
	t1 := time.Date(2026, 8, 20, 10, 10, 0, 0, time.UTC)
	t2 := t1.Add(20 * time.Minute)
	entries := []streamLogEntry{
		{ClientIP: "10.0.0.2", ProxyAddr: "2001:db8::1:25565", UpstreamAddr: "192.168.76.165:25565", Protocol: "TCP", Country: "Portugal", Status: 200, BytesSent: 100, BytesReceived: 50, SessionTime: 2, Timestamp: t1.Format(time.RFC3339), Time: t1},
		{ClientIP: "10.0.0.2", ProxyAddr: "2001:db8::1:25565", UpstreamAddr: "192.168.76.165:25565", Protocol: "TCP", Country: "Portugal", Status: 502, BytesSent: 300, BytesReceived: 50, SessionTime: 6, Timestamp: t2.Format(time.RFC3339), Time: t2},
	}
	aliases := buildAnalyticsAliasMap([]dnsmasq.AliasEntry{{Alias: "vm.minelive", IP: "192.168.76.165"}})
	streams := []npm.Stream{{
		ID: 7, Incoming_port: 25565, Forwarding_host: "game.vm.minelive", Forwarding_port: 25565,
		Tcp_forwarding: true, Enabled: true,
	}}
	filters := analyticsFilters{pathLimit: 10, sessionLimit: 10}

	response := aggregateStreamAnalytics(entries, aliases, streams, map[int]string{7: "Minecraft"}, true, filters)
	if response.Summary.TotalConnections != 2 || response.Summary.SuccessfulConnections != 1 || response.Summary.FailedConnections != 1 {
		t.Fatalf("summary = %#v", response.Summary)
	}
	if response.Summary.TotalBytes != 500 || response.Summary.AvgSession != 4 || response.Summary.MaxSession != 6 {
		t.Fatalf("summary metrics = %#v", response.Summary)
	}
	if len(response.Paths) != 1 {
		t.Fatalf("paths = %#v", response.Paths)
	}
	path := response.Paths[0]
	if path.StreamMatchStatus != "matched" || len(path.Streams) != 1 || path.Streams[0].Description != "Minecraft" {
		t.Fatalf("stream match = %q %#v", path.StreamMatchStatus, path.Streams)
	}
	if path.ObservedListener.IP != "2001:db8::1" || path.ObservedListener.Port != "25565" {
		t.Fatalf("unbracketed listener = %#v", path.ObservedListener)
	}
	if path.Destination.Display != "vm.minelive (192.168.76.165):25565" {
		t.Fatalf("destination display = %q", path.Destination.Display)
	}
	if path.FailureCount != 1 || path.FailureRate != 0.5 || path.AvgSession != 4 || path.MaxSession != 6 {
		t.Fatalf("path metrics = %#v", path)
	}
	if len(response.Destinations) != 1 || len(response.HourlyTimeline) != 1 || len(response.RecentSessions) != 2 {
		t.Fatalf("aggregate sizes: destinations=%d timeline=%d sessions=%d", len(response.Destinations), len(response.HourlyTimeline), len(response.RecentSessions))
	}
	if response.HourlyTimeline[0].UniqueSources != 1 || response.HourlyTimeline[0].UniqueDestinations != 1 {
		t.Fatalf("timeline unique counts = %#v", response.HourlyTimeline[0])
	}
	if response.RecentSessions[0].Status != 502 {
		t.Fatalf("recent sessions are not newest first: %#v", response.RecentSessions)
	}
	if response.RecentSessions[0].StreamMatchStatus != "matched" || len(response.RecentSessions[0].Streams) != 1 || response.RecentSessions[0].Streams[0].ID != 7 {
		t.Fatalf("recent session stream match = %q %#v", response.RecentSessions[0].StreamMatchStatus, response.RecentSessions[0].Streams)
	}
	if !strings.Contains(response.PathSemantics, "current configuration") {
		t.Fatalf("path semantics does not disclose current configuration: %q", response.PathSemantics)
	}
	if response.Metadata.Limits.BreakdownsAvailable["protocols"] != 1 || response.Metadata.Limits.BreakdownsTruncatedByDimension["protocols"] {
		t.Fatalf("breakdown limits = %#v", response.Metadata.Limits)
	}
	if len(response.Sources) != 1 {
		t.Fatalf("sources = %#v", response.Sources)
	}
	source := response.Sources[0]
	if source.Source.RawAddress != "10.0.0.2" || source.Connections != 2 || source.FailureCount != 1 || source.FailureRate != 0.5 || source.BytesSent != 400 || source.BytesReceived != 100 || source.TotalBytes != 500 || source.AvgSession != 4 || source.MaxSession != 6 || source.UniqueListeners != 1 || source.UniqueDestinations != 1 {
		t.Fatalf("source metrics = %#v", source)
	}
	if !reflect.DeepEqual(source.Protocols, []string{"TCP"}) || !reflect.DeepEqual(source.Countries, []string{"Portugal"}) || source.FirstSeen != t1.Format(time.RFC3339) || source.LastSeen != t2.Format(time.RFC3339) {
		t.Fatalf("source dimensions/times = %#v", source)
	}
	if len(response.Breakdowns.Statuses) != 2 || response.Breakdowns.Statuses[0].Value != "502" || response.Breakdowns.Statuses[1].Value != "200" {
		t.Fatalf("status breakdown = %#v", response.Breakdowns.Statuses)
	}
	if response.Metadata.Limits.BreakdownsAvailable["statuses"] != 2 || response.Metadata.Limits.BreakdownsReturned["statuses"] != 2 || response.Metadata.Limits.BreakdownsTruncatedByDimension["statuses"] {
		t.Fatalf("status limits = %#v", response.Metadata.Limits)
	}
}

func TestAnalyticsSourcesSortAndDimensions(t *testing.T) {
	entries := []streamLogEntry{
		{ClientIP: "source-c", ProxyAddr: "listener-1", UpstreamAddr: "destination-1", Protocol: "udp", Country: "US", Status: 200, BytesSent: 100},
		{ClientIP: "source-b", ProxyAddr: "listener-1", UpstreamAddr: "destination-1", Protocol: "tcp", Country: "PT", Status: 200, BytesSent: 100},
		{ClientIP: "source-a", ProxyAddr: "listener-1", UpstreamAddr: "destination-1", Protocol: "tcp", Country: "PT", Status: 200, BytesSent: 50},
		{ClientIP: "source-a", ProxyAddr: "listener-2", UpstreamAddr: "destination-2", Protocol: "udp", Country: "US", Status: 502, BytesSent: 50},
		{ClientIP: "source-d", ProxyAddr: "listener-1", UpstreamAddr: "destination-1", Protocol: "tcp", Country: "PT", Status: 200, BytesSent: 200},
	}
	response := aggregateStreamAnalytics(entries, map[string][]string{}, []npm.Stream{}, map[int]string{}, true, analyticsFilters{pathLimit: 10, sessionLimit: 10})
	wantOrder := []string{"source-a", "source-b", "source-c", "source-d"}
	for i, want := range wantOrder {
		if response.Sources[i].Source.RawAddress != want {
			t.Fatalf("source order at %d = %q; want %q", i, response.Sources[i].Source.RawAddress, want)
		}
	}
	if response.Sources[0].UniqueListeners != 2 || response.Sources[0].UniqueDestinations != 2 || !reflect.DeepEqual(response.Sources[0].Protocols, []string{"TCP", "UDP"}) || !reflect.DeepEqual(response.Sources[0].Countries, []string{"PT", "US"}) {
		t.Fatalf("source unique dimensions = %#v", response.Sources[0])
	}

}

func TestAnalyticsSourcePaginationIsComplete(t *testing.T) {
	const totalSources = 1201
	entries := make([]streamLogEntry, totalSources)
	for i := range entries {
		entries[i] = streamLogEntry{
			ClientIP: fmt.Sprintf("2001:db8::%x", i+1), ProxyAddr: "192.168.1.10:443", UpstreamAddr: "192.168.1.20:80",
			Protocol: "TCP", Country: "PT", Status: 200, BytesSent: int64(i + 1),
		}
	}

	seen := make(map[string]struct{}, totalSources)
	cursor := ""
	previousIdentity := ""
	havePrevious := false
	for {
		url := "/streamInfo/analytics?source_limit=500"
		if cursor != "" {
			url += "&source_cursor=" + cursor
		}
		filters, err := parseAnalyticsFilters(httptest.NewRequest(http.MethodGet, url, nil))
		if err != nil {
			t.Fatal(err)
		}
		filters.pathLimit = 1
		filters.sessionLimit = 1
		response := aggregateStreamAnalytics(entries, map[string][]string{}, []npm.Stream{}, map[int]string{}, true, filters)
		limits := response.Metadata.Limits
		if limits.SourceLimit != analyticsMaxSourceLimit || limits.SourceCursor != cursor || limits.SourcesReturned != len(response.Sources) || limits.SourcesAvailable != totalSources || !limits.SourcesTruncated {
			t.Fatalf("cursor page %q limits = %#v", cursor, limits)
		}
		for _, source := range response.Sources {
			identity := source.Source.RawAddress
			if havePrevious && identity <= previousIdentity {
				t.Fatalf("source ordering is not ascending: %q after %q", identity, previousIdentity)
			}
			previousIdentity = identity
			havePrevious = true
			if _, duplicate := seen[identity]; duplicate {
				t.Fatalf("duplicate paged source %q", identity)
			}
			seen[identity] = struct{}{}
		}
		if !limits.SourcesHasMore {
			if limits.SourcesNextCursor != "" {
				t.Fatalf("final page has next cursor %q", limits.SourcesNextCursor)
			}
			break
		}
		if limits.SourcesNextCursor == "" || limits.SourcesNextCursor == cursor {
			t.Fatalf("cursor did not advance: current %q, next %q", cursor, limits.SourcesNextCursor)
		}
		cursor = limits.SourcesNextCursor
	}
	if len(seen) != totalSources {
		t.Fatalf("paged sources = %d; want %d", len(seen), totalSources)
	}

	afterCursor := encodeAnalyticsSourceCursor("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff")
	afterEnd := aggregateStreamAnalytics(entries, map[string][]string{}, []npm.Stream{}, map[int]string{}, true, analyticsFilters{
		pathLimit: 1, sessionLimit: 1, sourceLimit: 100, sourceCursor: afterCursor, sourceAfter: "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff",
	})
	if len(afterEnd.Sources) != 0 || afterEnd.Metadata.Limits.SourcesReturned != 0 || afterEnd.Metadata.Limits.SourcesHasMore || afterEnd.Metadata.Limits.SourcesAvailable != totalSources {
		t.Fatalf("after-end source page = sources %d, limits %#v", len(afterEnd.Sources), afterEnd.Metadata.Limits)
	}
	if afterEnd.Metadata.Limits.SourceCursor != afterCursor || afterEnd.Metadata.Limits.SourcesNextCursor != "" {
		t.Fatalf("after-end cursors = %#v", afterEnd.Metadata.Limits)
	}
}

func TestAnalyticsSourceCursorStableAcrossAppends(t *testing.T) {
	entries := []streamLogEntry{
		{ClientIP: "source-b", Status: 200},
		{ClientIP: "source-d", Status: 200},
	}
	first := aggregateStreamAnalytics(entries, map[string][]string{}, []npm.Stream{}, map[int]string{}, true, analyticsFilters{pathLimit: 1, sessionLimit: 1, sourceLimit: 1})
	if len(first.Sources) != 1 || first.Sources[0].Source.RawAddress != "source-b" || first.Metadata.Limits.SourcesNextCursor == "" {
		t.Fatalf("first cursor page = sources %#v, limits %#v", first.Sources, first.Metadata.Limits)
	}

	entries = append(entries, streamLogEntry{ClientIP: "source-a", Status: 200}, streamLogEntry{ClientIP: "source-c", Status: 200})
	after, err := parseAnalyticsSourceCursor(first.Metadata.Limits.SourcesNextCursor)
	if err != nil {
		t.Fatal(err)
	}
	continued := aggregateStreamAnalytics(entries, map[string][]string{}, []npm.Stream{}, map[int]string{}, true, analyticsFilters{
		pathLimit: 1, sessionLimit: 1, sourceLimit: 1, sourceCursor: first.Metadata.Limits.SourcesNextCursor, sourceAfter: after,
	})
	if len(continued.Sources) != 1 || continued.Sources[0].Source.RawAddress != "source-c" {
		t.Fatalf("continued cursor page = %#v", continued.Sources)
	}
	fresh := aggregateStreamAnalytics(entries, map[string][]string{}, []npm.Stream{}, map[int]string{}, true, analyticsFilters{pathLimit: 1, sessionLimit: 1, sourceLimit: 1})
	if len(fresh.Sources) != 1 || fresh.Sources[0].Source.RawAddress != "source-a" {
		t.Fatalf("fresh cursor traversal = %#v", fresh.Sources)
	}
}

func TestAnalyticsCanonicalSourceIdentity(t *testing.T) {
	hour := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		{ClientIP: "2001:0db8:0:0:0:0:0:1", ProxyAddr: "192.168.1.10:443", UpstreamAddr: "192.168.1.20:80", Protocol: "TCP", Country: "PT", Status: 200, BytesSent: 100, BytesReceived: 25, SessionTime: 2, Time: hour.Add(5 * time.Minute)},
		{ClientIP: "2001:db8::1", ProxyAddr: "192.168.1.10:443", UpstreamAddr: "192.168.1.20:80", Protocol: "TCP", Country: "US", Status: 502, BytesSent: 200, BytesReceived: 75, SessionTime: 6, Time: hour.Add(10 * time.Minute)},
	}
	response := aggregateStreamAnalytics(entries, map[string][]string{}, []npm.Stream{}, map[int]string{}, true, analyticsFilters{pathLimit: 10, sessionLimit: 10})
	if response.Summary.UniqueSources != 1 || len(response.Sources) != 1 {
		t.Fatalf("canonical source counts = summary %d, sources %#v", response.Summary.UniqueSources, response.Sources)
	}
	source := response.Sources[0]
	if source.Source.RawAddress != "2001:db8::1" || source.Source.IP != "2001:db8::1" || source.Connections != 2 || source.FailureCount != 1 || source.BytesSent != 300 || source.BytesReceived != 100 || source.TotalBytes != 400 || source.AvgSession != 4 || source.MaxSession != 6 || source.UniqueListeners != 1 || source.UniqueDestinations != 1 {
		t.Fatalf("canonical source aggregate = %#v", source)
	}
	if len(response.Paths) != 1 || response.Paths[0].Connections != 2 || len(response.Destinations) != 1 || response.Destinations[0].UniqueSources != 1 || len(response.HourlyTimeline) != 1 || response.HourlyTimeline[0].UniqueSources != 1 {
		t.Fatalf("canonical route aggregates = paths %#v, destinations %#v, timeline %#v", response.Paths, response.Destinations, response.HourlyTimeline)
	}
	pathEntries := filterAnalyticsEntries(entries, analyticsFilters{pathID: response.Paths[0].ID}, map[string][]string{})
	if len(pathEntries) != 2 {
		t.Fatalf("canonical path drill returned %#v", pathEntries)
	}

	req := httptest.NewRequest(http.MethodGet, "/streamInfo/analytics?source_ip=2001:0db8::1", nil)
	filters, err := parseAnalyticsFilters(req)
	if err != nil {
		t.Fatal(err)
	}
	filtered := filterAnalyticsEntries(entries, filters, map[string][]string{})
	filteredResponse := aggregateStreamAnalytics(filtered, map[string][]string{}, []npm.Stream{}, map[int]string{}, true, analyticsFilters{pathLimit: 10, sessionLimit: 10})
	if len(filtered) != 2 || len(filteredResponse.Sources) != 1 || filteredResponse.Sources[0].Connections != 2 || filteredResponse.Metadata.Limits.SourcesAvailable != 1 || filteredResponse.Metadata.Limits.SourcesReturned != 1 || filteredResponse.Metadata.Limits.SourcesHasMore {
		t.Fatalf("exact canonical source filter returned entries=%d sources=%#v", len(filtered), filteredResponse.Sources)
	}
}

func TestAnalyticsStreamMatchingEvidence(t *testing.T) {
	listener := enrichAnalyticsListener("2001:db8::1:443", map[string][]string{})
	destination := enrichAnalyticsEndpoint("192.168.76.165:8443", map[string][]string{})
	stream := npm.Stream{
		ID: 9, Incoming_port: 443, Forwarding_host: "unmanaged.example", Forwarding_port: 8443,
		Tcp_forwarding: true, Enabled: true,
	}

	matches, status := matchAnalyticsStreams(listener, destination, "TCP", map[string][]string{}, []npm.Stream{stream}, map[int]string{}, true)
	if status != "indeterminate" || len(matches) != 1 || matches[0].ID != 9 {
		t.Fatalf("unmanaged hostname result = %q %#v", status, matches)
	}

	stream.Forwarding_host = "192.168.76.165"
	matches, status = matchAnalyticsStreams(listener, destination, "SCTP", map[string][]string{}, []npm.Stream{stream}, map[int]string{}, true)
	if status != "indeterminate" || len(matches) != 1 || analyticsStreamProtocolMatches(stream, "SCTP") {
		t.Fatalf("unknown protocol result = %q %#v", status, matches)
	}

	stream.Forwarding_host = "192.168.76.166"
	matches, status = matchAnalyticsStreams(listener, destination, "TCP", map[string][]string{}, []npm.Stream{stream}, map[int]string{}, true)
	if status != "unmatched" || len(matches) != 0 {
		t.Fatalf("different configured IP result = %q %#v", status, matches)
	}
}

func TestAnalyticsStreamMatchingMixedEvidenceIsAmbiguous(t *testing.T) {
	listener := enrichAnalyticsListener("192.168.1.10:443", map[string][]string{})
	destination := enrichAnalyticsEndpoint("192.168.76.165:8443", map[string][]string{})
	proven := npm.Stream{
		ID: 30, Incoming_port: 443, Forwarding_host: "192.168.76.165", Forwarding_port: 8443,
		Tcp_forwarding: true,
	}
	indeterminate := npm.Stream{
		ID: 10, Incoming_port: 443, Forwarding_host: "unmanaged.example", Forwarding_port: 8443,
		Tcp_forwarding: true,
	}

	matches, status := matchAnalyticsStreams(listener, destination, "TCP", map[string][]string{}, []npm.Stream{proven, indeterminate}, map[int]string{}, true)
	if status != "ambiguous" || len(matches) != 2 || matches[0].ID != 10 || matches[1].ID != 30 {
		t.Fatalf("one proven plus indeterminate = %q %#v", status, matches)
	}

	secondProven := proven
	secondProven.ID = 20
	matches, status = matchAnalyticsStreams(listener, destination, "TCP", map[string][]string{}, []npm.Stream{proven, indeterminate, secondProven}, map[int]string{}, true)
	if status != "ambiguous" || len(matches) != 3 || matches[0].ID != 10 || matches[1].ID != 20 || matches[2].ID != 30 {
		t.Fatalf("multiple proven plus indeterminate = %q %#v", status, matches)
	}
}

func TestAnalyticsCollectionCaps(t *testing.T) {
	destinations := []analyticsDestination{
		{Destination: analyticsEndpoint{RawAddress: "first"}},
		{Destination: analyticsEndpoint{RawAddress: "second"}},
		{Destination: analyticsEndpoint{RawAddress: "third"}},
	}
	cappedDestinations, destinationsAvailable := capAnalyticsDestinations(destinations, 2)
	if len(cappedDestinations) != 2 || destinationsAvailable != 3 || !analyticsCollectionTruncated(destinationsAvailable, len(cappedDestinations)) || cappedDestinations[1].Destination.RawAddress != "second" {
		t.Fatalf("destination cap = %d/%d %#v", len(cappedDestinations), destinationsAvailable, cappedDestinations)
	}

	timeline := []analyticsTimelinePoint{{Timestamp: "one"}, {Timestamp: "two"}, {Timestamp: "three"}}
	cappedTimeline, timelineAvailable := capLatestAnalyticsTimeline(timeline, 2)
	if len(cappedTimeline) != 2 || timelineAvailable != 3 || !analyticsCollectionTruncated(timelineAvailable, len(cappedTimeline)) || cappedTimeline[0].Timestamp != "two" {
		t.Fatalf("timeline cap = %d/%d %#v", len(cappedTimeline), timelineAvailable, cappedTimeline)
	}

	breakdowns := map[string]*analyticsBreakdownItem{
		"small":  {Value: "small", TotalBytes: 1},
		"large":  {Value: "large", TotalBytes: 3},
		"medium": {Value: "medium", TotalBytes: 2},
	}
	cappedBreakdowns, breakdownsAvailable := analyticsBreakdownSlice(breakdowns, 2)
	if len(cappedBreakdowns) != 2 || breakdownsAvailable != 3 || !analyticsCollectionTruncated(breakdownsAvailable, len(cappedBreakdowns)) || cappedBreakdowns[0].Value != "large" || cappedBreakdowns[1].Value != "medium" {
		t.Fatalf("breakdown cap = %d/%d %#v", len(cappedBreakdowns), breakdownsAvailable, cappedBreakdowns)
	}
}

func TestAnalyticsTimelineUniqueCounts(t *testing.T) {
	hour := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	entries := []streamLogEntry{
		{ClientIP: "10.0.0.2", ProxyAddr: "192.168.1.10:443", UpstreamAddr: "192.168.76.10:80", Protocol: "TCP", Status: 200, Time: hour.Add(5 * time.Minute)},
		{ClientIP: "10.0.0.2", ProxyAddr: "192.168.1.10:443", UpstreamAddr: "192.168.76.11:80", Protocol: "TCP", Status: 200, Time: hour.Add(10 * time.Minute)},
		{ClientIP: "10.0.0.3", ProxyAddr: "192.168.1.10:443", UpstreamAddr: "192.168.76.11:80", Protocol: "TCP", Status: 200, Time: hour.Add(15 * time.Minute)},
	}
	response := aggregateStreamAnalytics(entries, map[string][]string{}, []npm.Stream{}, map[int]string{}, true, analyticsFilters{pathLimit: 10, sessionLimit: 10})
	if len(response.HourlyTimeline) != 1 {
		t.Fatalf("timeline = %#v", response.HourlyTimeline)
	}
	point := response.HourlyTimeline[0]
	if point.UniqueSources != 2 || point.UniqueDestinations != 2 {
		t.Fatalf("unique counts = %d/%d; want 2/2", point.UniqueSources, point.UniqueDestinations)
	}
}

func TestStreamAnalyticsHandlerReadsOnceAndReturnsEmptyArrays(t *testing.T) {
	loads := 0
	deps := analyticsDependencies{
		loadEntries: func() ([]streamLogEntry, *geoip2.Reader, error) {
			loads++
			return []streamLogEntry{}, nil, nil
		},
		getAliases:  func() ([]dnsmasq.AliasEntry, error) { return []dnsmasq.AliasEntry{}, nil },
		listStreams: func(string, string) ([]npm.Stream, error) { return []npm.Stream{}, nil },
		getDescriptions: func(context.Context, string, []int) (map[int]string, error) {
			return map[int]string{}, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/streamInfo/analytics", nil)
	recorder := httptest.NewRecorder()
	serveStreamAnalytics(recorder, req, deps)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if loads != 1 {
		t.Fatalf("loadEntries called %d times", loads)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"paths", "sources", "destinations", "hourly_timeline", "recent_sessions"} {
		value, ok := body[field].([]any)
		if !ok || len(value) != 0 {
			t.Errorf("%s = %#v; want []", field, body[field])
		}
	}
	breakdowns := body["breakdowns"].(map[string]any)
	for _, field := range []string{"protocols", "countries", "ports", "outcomes", "statuses", "source_scopes", "destination_scopes"} {
		if value, ok := breakdowns[field].([]any); !ok || len(value) != 0 {
			t.Errorf("breakdowns.%s = %#v; want []", field, breakdowns[field])
		}
	}
	for _, field := range []string{"path_semantics", "metadata", "summary", "paths", "sources", "destinations", "hourly_timeline", "recent_sessions", "breakdowns"} {
		if _, ok := body[field]; !ok {
			t.Errorf("top-level JSON field %q is missing", field)
		}
	}
	metadata := body["metadata"].(map[string]any)
	limits := metadata["limits"].(map[string]any)
	for _, field := range []string{"source_limit", "source_cursor", "sources_returned", "sources_available", "sources_has_more", "sources_next_cursor", "sources_truncated"} {
		if _, ok := limits[field]; !ok {
			t.Errorf("metadata.limits JSON field %q is missing", field)
		}
	}
	available := limits["breakdowns_available"].(map[string]any)
	if _, ok := available["statuses"]; !ok {
		t.Error("metadata.limits.breakdowns_available.statuses is missing")
	}
}

func TestStreamAnalyticsHandlerRejectsInvalidQueryBeforeLoading(t *testing.T) {
	for _, query := range []string{
		"start=bad",
		"path_id=path_ABCDEF0123456789",
		"source_ip=invalid",
		"listener_ip=invalid",
		"destination_ip=invalid",
		"source_limit=0",
		"source_cursor=not*a*cursor",
		"source_cursor=" + strings.Repeat("a", analyticsMaxSourceCursorSize+1),
	} {
		t.Run(query, func(t *testing.T) {
			deps := analyticsDependencies{
				loadEntries: func() ([]streamLogEntry, *geoip2.Reader, error) {
					t.Fatal("loadEntries must not run for an invalid query")
					return nil, nil, errors.New("unreachable")
				},
			}
			req := httptest.NewRequest(http.MethodGet, "/streamInfo/analytics?"+query, nil)
			recorder := httptest.NewRecorder()
			serveStreamAnalytics(recorder, req, deps)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestStreamAnalyticsHandlerUnknownPathIDReturnsEmptyOK(t *testing.T) {
	deps := analyticsDependencies{
		loadEntries: func() ([]streamLogEntry, *geoip2.Reader, error) {
			return []streamLogEntry{{ClientIP: "10.0.0.2", ProxyAddr: "192.168.1.10:443", UpstreamAddr: "192.168.1.20:80", Protocol: "TCP", Status: 200}}, nil, nil
		},
		getAliases:  func() ([]dnsmasq.AliasEntry, error) { return []dnsmasq.AliasEntry{}, nil },
		listStreams: func(string, string) ([]npm.Stream, error) { return []npm.Stream{}, nil },
		getDescriptions: func(context.Context, string, []int) (map[int]string, error) {
			return map[int]string{}, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/streamInfo/analytics?path_id=path_0000000000000000", nil)
	recorder := httptest.NewRecorder()
	serveStreamAnalytics(recorder, req, deps)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response analyticsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Metadata.FilteredEntries != 0 || len(response.Paths) != 0 || len(response.Sources) != 0 {
		t.Fatalf("unknown path response = %#v", response)
	}
}

func TestStreamAnalyticsHandlerDegradesOptionalDependencies(t *testing.T) {
	deps := analyticsDependencies{
		loadEntries: func() ([]streamLogEntry, *geoip2.Reader, error) {
			return []streamLogEntry{}, nil, nil
		},
		getAliases: func() ([]dnsmasq.AliasEntry, error) {
			return nil, errors.New("aliases unavailable")
		},
		listStreams: func(string, string) ([]npm.Stream, error) {
			return nil, errors.New("NPM unavailable")
		},
		getDescriptions: func(context.Context, string, []int) (map[int]string, error) {
			t.Fatal("descriptions must not be loaded when NPM is unavailable")
			return nil, errors.New("unreachable")
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/streamInfo/analytics", nil)
	recorder := httptest.NewRecorder()
	serveStreamAnalytics(recorder, req, deps)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var response analyticsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Metadata.Availability.GeoIP || response.Metadata.Availability.DNSAliases || response.Metadata.Availability.NPMStreams || response.Metadata.Availability.Descriptions {
		t.Fatalf("availability = %#v", response.Metadata.Availability)
	}
	if len(response.Metadata.Warnings) != 4 {
		t.Fatalf("warnings = %#v", response.Metadata.Warnings)
	}
	foundCurrentStateWarning := false
	for _, warning := range response.Metadata.Warnings {
		foundCurrentStateWarning = foundCurrentStateWarning || strings.Contains(warning, "current configuration")
	}
	if !foundCurrentStateWarning {
		t.Fatalf("current-state warning missing: %#v", response.Metadata.Warnings)
	}
}

func TestStreamAnalyticsMetadataCounts(t *testing.T) {
	entries := []streamLogEntry{
		{ClientIP: "10.0.0.2", ProxyAddr: "192.168.1.10:443", UpstreamAddr: "192.168.76.10:80", Protocol: "TCP", Status: 200},
		{ClientIP: "10.0.0.3", ProxyAddr: "192.168.1.10:443", UpstreamAddr: "192.168.76.11:80", Protocol: "TCP", Status: 502},
	}
	deps := analyticsDependencies{
		loadEntries: func() ([]streamLogEntry, *geoip2.Reader, error) {
			return entries, nil, nil
		},
		getAliases: func() ([]dnsmasq.AliasEntry, error) {
			return []dnsmasq.AliasEntry{
				{Alias: "one.local", IP: "192.168.76.10"},
				{Alias: "one.local", IP: "192.168.76.10"},
				{Alias: "two.local", IP: "192.168.76.11"},
			}, nil
		},
		listStreams: func(string, string) ([]npm.Stream, error) { return []npm.Stream{}, nil },
		getDescriptions: func(context.Context, string, []int) (map[int]string, error) {
			return map[int]string{}, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/streamInfo/analytics?outcome=success", nil)
	recorder := httptest.NewRecorder()
	serveStreamAnalytics(recorder, req, deps)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var response analyticsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Metadata.TotalAvailableEntries != 2 || response.Metadata.FilteredEntries != 1 {
		t.Fatalf("metadata entry counts = %d/%d; want 2/1", response.Metadata.TotalAvailableEntries, response.Metadata.FilteredEntries)
	}
	if response.Metadata.AliasesCount != 2 {
		t.Fatalf("aliases_count = %d; want 2", response.Metadata.AliasesCount)
	}
	generatedAt, err := time.Parse(time.RFC3339, response.Metadata.GeneratedAt)
	if err != nil || generatedAt.Location() != time.UTC {
		t.Fatalf("generated_at = %q, parse error = %v", response.Metadata.GeneratedAt, err)
	}
}
