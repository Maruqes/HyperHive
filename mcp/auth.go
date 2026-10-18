package hyperhivemcp

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// requireMCPToken accepts only an explicit Bearer header. The fixed MCP key is
// never accepted through query parameters or cookies.
func requireMCPToken(next http.Handler, token string) http.Handler {
	token = strings.TrimSpace(token)
	configured := token != "" && !strings.ContainsAny(token, " \t\r\n")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !configured {
			http.Error(w, "MCP_TOKEN is not configured", http.StatusServiceUnavailable)
			return
		}
		if !MatchesMCPToken(r, token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hyperhive-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// MatchesMCPToken is shared by MCP HTTP and the API's alternate authentication.
// Empty configuration never enables access. Only a single Bearer header counts.
func MatchesMCPToken(r *http.Request, token string) bool {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return false
	}
	scheme, candidate, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || candidate == "" {
		return false
	}
	return MatchesMCPKey(candidate, token)
}

// MatchesMCPKey compares a candidate key without implying any allowed transport.
// API callers use it to reject the fixed key in legacy cookie/query auth paths.
func MatchesMCPKey(candidate, token string) bool {
	token = strings.TrimSpace(token)
	if candidate == "" || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return false
	}
	expected := sha256.Sum256([]byte(token))
	actual := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(actual[:], expected[:]) == 1
}
