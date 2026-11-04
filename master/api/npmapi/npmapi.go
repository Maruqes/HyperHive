package npmapi

import "net/http"

var baseURL string

var apiPort = 9595

// SetAPIPort keeps newly generated frontend proxy targets on the API listener.
func SetAPIPort(port int) {
	apiPort = port
}

func SetBaseURL(url string) {
	baseURL = url
}

// BaseURL returns the local NPM API address for callers outside this package.
func BaseURL() string {
	return baseURL
}

// copied from login, there is no problem couse it should exist in context
func GetTokenFromContext(r *http.Request) string {
	if token, ok := r.Context().Value("token").(string); ok {
		return token
	}
	return ""
}
