package api

import (
	"512SvMan/services"
	"context"
	"encoding/json"
	"net/http"
)

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	loginService := services.LoginService{}
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

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
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
