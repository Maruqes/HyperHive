package api

import (
	"512SvMan/services"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
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
	_ = json.NewEncoder(w).Encode(LoginResponse{Token: token})
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

func tryGetFromURLparam(r *http.Request) string {
	return normalizeTokenValue(r.URL.Query().Get("token"))
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := tokenFromRequest(r)

		// Also check query parameters for token
		if token == "" {
			token = normalizeTokenValue(r.URL.Query().Get("token"))
		}

		if token == "" {
			token = tryGetFromURLparam(r)
		}

		if token == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		loginService := services.LoginService{}
		if !loginService.IsLoginValid(baseURL, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Add token to request context
		r = SetTokenInContext(r, token)

		// continue to the next handler
		next.ServeHTTP(w, r)
	})
}
