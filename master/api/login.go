package api

import (
	"512SvMan/npm"
	"512SvMan/services"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	hyperhivemcp "github.com/Maruqes/HyperHive/mcp"
)

type LoginResponse struct {
	Token string `json:"token"`
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	loginService := services.LoginService{}

	type LoginRequest struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	token, err := loginService.Login(baseURL, req.Email, req.Password)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(LoginResponse{Token: token}); err != nil {
		log.Printf("login: encode response failed: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
}

func SetTokenInContext(r *http.Request, token string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), "token", token))
}

func GetTokenFromContext(r *http.Request) string {
	if token, ok := r.Context().Value("token").(string); ok {
		return token
	}
	return ""
}

func normalizeTokenValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	// Authorization headers typically arrive as "Bearer <token>"
	bearerPrefix := "Bearer "
	if len(value) >= len(bearerPrefix) && strings.EqualFold(value[:len(bearerPrefix)], bearerPrefix) {
		return strings.TrimSpace(value[len(bearerPrefix):])
	}

	return value
}

func tokenFromRequest(r *http.Request) string {
	headerToken := normalizeTokenValue(r.Header.Get("Authorization"))
	if headerToken != "" {
		return headerToken
	}

	cookie, err := r.Cookie("Authorization")
	if err != nil {
		return ""
	}

	cookieValue := strings.TrimSpace(cookie.Value)
	if strings.Contains(cookieValue, "%") {
		if decoded, err := url.QueryUnescape(cookieValue); err == nil {
			cookieValue = decoded
		}
	}

	return normalizeTokenValue(cookieValue)
}

func isAuthorized(r *http.Request) (bool, string) {
	token, err := authenticateRequest(r)
	return err == nil, token
}

var errUnauthorized = errors.New("unauthorized")

func authenticateRequest(r *http.Request) (string, error) {
	if hyperhivemcp.MatchesMCPToken(r, os.Getenv("MCP_TOKEN")) {
		// Downstream NPM handlers still receive an NPM-compatible credential,
		// never the fixed MCP key. No manual API session is needed in the .env.
		return npm.ServiceToken(r.Context(), "npm-data")
	}
	token := tokenFromRequest(r)

	// Also check query parameters for token
	if token == "" {
		token = normalizeTokenValue(r.URL.Query().Get("token"))
	}

	if token == "" {
		return "", errUnauthorized
	}
	if hyperhivemcp.MatchesMCPKey(token, os.Getenv("MCP_TOKEN")) {
		// A fixed key in a cookie/query/raw header must not leak to NPM via the
		// legacy session validator. Only the explicit Bearer branch accepts it.
		return "", errUnauthorized
	}

	loginService := services.LoginService{}
	if !loginService.IsLoginValid(baseURL, token) {
		return "", errUnauthorized
	}
	return token, nil
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, err := authenticateRequest(r)
		if err != nil {
			applyCORSHeaders(w, r)
			if errors.Is(err, errUnauthorized) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			} else {
				http.Error(w, "MCP backend authentication is unavailable; check the local NPM installation", http.StatusServiceUnavailable)
			}
			return
		}

		// Add token to request context
		r = SetTokenInContext(r, token)

		// continue to the next handler
		next.ServeHTTP(w, r)
	})
}
