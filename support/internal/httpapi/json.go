package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"hyperhive/support/internal/domain"
)

func respond(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(writer).Encode(value)
	}
}

func readBody(writer http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, 1<<20)
	data, err := io.ReadAll(request.Body)
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			return domain.TooLarge("request exceeds 1 MiB")
		}
		return domain.Invalid("could not read request")
	}
	if strings.TrimSpace(string(data)) == "null" || !utf8.Valid(data) || !json.Valid(data) {
		return domain.Invalid("invalid JSON body")
	}
	if err := domain.StrictJSON(data, target); err != nil {
		return domain.Invalid("invalid JSON body: " + err.Error())
	}
	return nil
}
