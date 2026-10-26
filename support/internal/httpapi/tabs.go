package httpapi

import (
	"net/http"

	"hyperhive/support/internal/domain"
)

func (handler *Handler) tabsRoute(writer http.ResponseWriter, request *http.Request, parts []string) error {
	if len(parts) > 2 {
		return domain.NotFound()
	}
	if len(parts) == 1 && request.Method == "GET" {
		tabs, err := handler.service.Tabs()
		if err != nil {
			return err
		}
		respond(writer, 200, tabs)
		return nil
	}
	if len(parts) == 1 && request.Method == "POST" || len(parts) == 2 && request.Method == "PUT" {
		var body struct {
			Name string `json:"name"`
		}
		if err := readBody(writer, request, &body); err != nil {
			return err
		}
		id := ""
		status := 201
		if len(parts) == 2 {
			id = parts[1]
			status = 200
		}
		tab, err := handler.service.SaveTab(id, body.Name)
		if err != nil {
			return err
		}
		respond(writer, status, tab)
		return nil
	}
	if len(parts) == 2 && request.Method == "DELETE" {
		if err := handler.service.DeleteTab(parts[1]); err != nil {
			return err
		}
		respond(writer, 204, nil)
		return nil
	}
	return domain.NotFound()
}
