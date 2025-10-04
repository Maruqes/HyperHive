package npmapi

import "net/http"

var baseURL string

func SetBaseURL(url string) {
	baseURL = url
}

// copied from login, there is no problem couse it should exist in context
func GetTokenFromContext(r *http.Request) string {
	if token, ok := r.Context().Value("token").(string); ok {
		return token
	}
	return ""
}
