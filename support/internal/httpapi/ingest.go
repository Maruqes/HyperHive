package httpapi

import (
	"net/http"
	"strings"

	"hyperhive/support/internal/domain"
)

func (handler *Handler) ingest(writer http.ResponseWriter, request *http.Request) error {
	id := request.Header.Get("X-App-ID")
	authorization := request.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		return domain.Unauthorized("invalid App credentials")
	}
	secret := strings.TrimPrefix(authorization, "Bearer ")
	if err := handler.service.AuthenticateApp(id, secret); err != nil {
		return err
	}
	var body domain.IngestRequest
	if err := readBody(writer, request, &body); err != nil {
		return err
	}
	count, err := handler.service.Ingest(request.Context(), id, secret, body)
	if err != nil {
		return err
	}
	respond(writer, 200, map[string]int{"accepted": count})
	return nil
}
