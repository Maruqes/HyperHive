package api

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"512SvMan/dnsmasq"
)

var observabilityAliasPath = "npm-data/stream-ip-aliases.json"

var observabilityAliasMu sync.Mutex

type observabilityAlias struct {
	IP    string `json:"ip"`
	Alias string `json:"alias"`
}

func loadObservabilityAliases() ([]observabilityAlias, error) {
	data, err := os.ReadFile(observabilityAliasPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []observabilityAlias{}, nil
		}
		return nil, err
	}
	var aliases []observabilityAlias
	if err := json.Unmarshal(data, &aliases); err != nil {
		return nil, err
	}
	return aliases, nil
}

func saveObservabilityAliases(aliases []observabilityAlias) error {
	sort.Slice(aliases, func(i, j int) bool {
		if aliases[i].IP != aliases[j].IP {
			return aliases[i].IP < aliases[j].IP
		}
		return aliases[i].Alias < aliases[j].Alias
	})
	data, err := json.MarshalIndent(aliases, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(observabilityAliasPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(observabilityAliasPath, data, 0o644)
}

func validateObservabilityAlias(alias observabilityAlias) error {
	if net.ParseIP(alias.IP) == nil {
		return errInvalidObservabilityIP
	}
	if alias.Alias == "" || strings.ContainsAny(alias.Alias, " \t\r\n,") {
		return errInvalidObservabilityAlias
	}
	return nil
}

var (
	errInvalidObservabilityIP    = errorString("ip must be a valid IP address")
	errInvalidObservabilityAlias = errorString("alias is required and cannot contain spaces or commas")
)

type errorString string

func (e errorString) Error() string { return string(e) }

func listObservabilityAliases(w http.ResponseWriter, r *http.Request) {
	observabilityAliasMu.Lock()
	aliases, err := loadObservabilityAliases()
	observabilityAliasMu.Unlock()
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to load aliases")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"aliases": aliases})
}

func addObservabilityAlias(w http.ResponseWriter, r *http.Request) {
	var req observabilityAlias
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.IP = strings.TrimSpace(req.IP)
	req.Alias = strings.TrimSpace(req.Alias)
	if ip := net.ParseIP(req.IP); ip != nil {
		req.IP = ip.String()
	}
	if err := validateObservabilityAlias(req); err != nil {
		respondJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	observabilityAliasMu.Lock()
	aliases, err := loadObservabilityAliases()
	if err == nil {
		found := false
		for i := range aliases {
			if aliases[i].IP == req.IP && aliases[i].Alias == req.Alias {
				found = true
				break
			}
			if aliases[i].IP == req.IP {
				aliases[i].Alias = req.Alias
				found = true
				break
			}
		}
		if !found {
			aliases = append(aliases, req)
		}
		err = saveObservabilityAliases(aliases)
	}
	observabilityAliasMu.Unlock()
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to save alias")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"saved": true})
}

func removeObservabilityAlias(w http.ResponseWriter, r *http.Request) {
	var req observabilityAlias
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.IP = strings.TrimSpace(req.IP)
	req.Alias = strings.TrimSpace(req.Alias)
	observabilityAliasMu.Lock()
	aliases, err := loadObservabilityAliases()
	if err == nil {
		filtered := make([]observabilityAlias, 0, len(aliases))
		for _, alias := range aliases {
			if alias.IP == req.IP && (req.Alias == "" || alias.Alias == req.Alias) {
				continue
			}
			filtered = append(filtered, alias)
		}
		if len(filtered) == len(aliases) {
			err = errObservabilityAliasNotFound
		} else {
			err = saveObservabilityAliases(filtered)
		}
	}
	observabilityAliasMu.Unlock()
	if err == errObservabilityAliasNotFound {
		respondJSONError(w, http.StatusNotFound, "alias not found")
		return
	}
	if err != nil {
		respondJSONError(w, http.StatusInternalServerError, "failed to remove alias")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"deleted": true})
}

var errObservabilityAliasNotFound = errorString("alias not found")

// getCombinedAliases merges DNS-managed aliases with observability aliases so
// dashboards, search and profiles all resolve the same names.
func getCombinedAliases() ([]dnsmasq.AliasEntry, error) {
	combined, dnsErr := dnsmasq.GetAllAliases()
	observability, obsErr := loadObservabilityAliases()
	for _, alias := range observability {
		combined = append(combined, dnsmasq.AliasEntry{Alias: alias.Alias, IP: alias.IP})
	}
	if dnsErr != nil && obsErr != nil {
		return combined, dnsErr
	}
	return combined, nil
}
