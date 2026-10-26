package httpapi

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"hyperhive/support/internal/domain"
	"hyperhive/support/internal/realtime"
	"hyperhive/support/internal/security"
	"hyperhive/support/internal/service"
)

type Handler struct {
	service *service.Service
	hub     *realtime.Hub
	secret  string
}

func New(service *service.Service, hub *realtime.Hub, secret string) (*Handler, error) {
	if len(secret) < 32 {
		return nil, errors.New("SUPPORT_PROXY_SECRET must contain at least 32 bytes")
	}
	return &Handler{service: service, hub: hub, secret: secret}, nil
}
func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if !(request.Method == "POST" && request.URL.Path == "/ingest") && !(request.Method == "GET" && request.URL.Path == "/healthz") && !security.Equal([]byte(request.Header.Get("X-HH-Support-Key")), []byte(handler.secret)) {
		respond(writer, 401, map[string]string{"error": "unauthorized"})
		return
	}
	if request.Method == "GET" && request.URL.Path == "/ws" {
		handler.hub.ServeHTTP(writer, request)
		return
	}
	if err := handler.route(writer, request); err != nil {
		var problem *domain.Error
		if errors.As(err, &problem) {
			respond(writer, errorStatus(problem.Kind), map[string]string{"error": problem.Message})
		} else {
			log.Printf("support request failed: %v", err)
			respond(writer, 500, map[string]string{"error": "internal service error"})
		}
	}
}
func (handler *Handler) route(writer http.ResponseWriter, request *http.Request) error {
	path := strings.Trim(request.URL.Path, "/")
	parts := strings.Split(path, "/")
	if path == "healthz" && request.Method == "GET" {
		if err := handler.service.Ping(request.Context()); err != nil {
			return domain.Unavailable("database unavailable")
		}
		respond(writer, 200, map[string]string{"status": "ok"})
		return nil
	}
	if path == "ingest" && request.Method == "POST" {
		return handler.ingest(writer, request)
	}
	if parts[0] == "tabs" {
		return handler.tabsRoute(writer, request, parts)
	}
	if parts[0] == "apps" {
		return handler.appsRoute(writer, request, parts)
	}
	return domain.NotFound()
}

func errorStatus(kind domain.ErrorKind) int {
	switch kind {
	case domain.InvalidInput:
		return http.StatusBadRequest
	case domain.ConflictState:
		return http.StatusConflict
	case domain.MissingResource:
		return http.StatusNotFound
	case domain.AuthenticationRequired:
		return http.StatusUnauthorized
	case domain.ServiceUnavailable:
		return http.StatusServiceUnavailable
	case domain.PayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusInternalServerError
	}
}
