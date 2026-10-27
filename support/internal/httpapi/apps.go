package httpapi

import (
	"net/http"
	"slices"

	"hyperhive/support/internal/domain"
)

func (handler *Handler) appsRoute(writer http.ResponseWriter, request *http.Request, parts []string) error {
	if len(parts) == 1 {
		switch request.Method {
		case "GET":
			apps, err := handler.service.Apps()
			if err != nil {
				return err
			}
			filtered := []domain.App{}
			for _, app := range apps {
				if tab := request.URL.Query().Get("tab_id"); tab != "" && tab != app.TabID {
					continue
				}
				if tag := request.URL.Query().Get("tag"); tag != "" && !slices.Contains(app.Tags, tag) {
					continue
				}
				filtered = append(filtered, app)
			}
			respond(writer, 200, filtered)
			return nil
		case "POST":
			return handler.saveApp(writer, request, "")
		}
		return domain.NotFound()
	}
	id := parts[1]
	if len(parts) == 2 {
		switch request.Method {
		case "GET":
			detail, err := handler.service.Detail(id)
			if err != nil {
				return err
			}
			respond(writer, 200, detail)
			return nil
		case "PUT":
			return handler.saveApp(writer, request, id)
		case "DELETE":
			if err := handler.service.DeleteApp(id); err != nil {
				return err
			}
			respond(writer, 204, nil)
			return nil
		}
		return domain.NotFound()
	}
	if len(parts) != 3 {
		return domain.NotFound()
	}
	switch parts[2] {
	case "replicate":
		if request.Method != "POST" {
			return domain.NotFound()
		}
		var body domain.ReplicateAppSettings
		if err := readBody(writer, request, &body); err != nil {
			return err
		}
		app, secret, err := handler.service.ReplicateApp(id, body)
		if err != nil {
			return err
		}
		respond(writer, 201, map[string]any{"app": app, "secret": secret})
		return nil
	case "rotate-secret":
		if request.Method != "POST" {
			return domain.NotFound()
		}
		secret, err := handler.service.RotateSecret(id)
		if err != nil {
			return err
		}
		respond(writer, 200, map[string]string{"secret": secret})
		return nil
	case "streams":
		if request.Method != "GET" {
			return domain.NotFound()
		}
		streams, err := handler.service.Streams(id)
		if err != nil {
			return err
		}
		respond(writer, 200, streams)
		return nil
	case "samples":
		if request.Method != "GET" {
			return domain.NotFound()
		}
		return handler.history(writer, request, id)
	case "layout":
		if request.Method == "GET" {
			layout, err := handler.service.Layout(id)
			if err != nil {
				return err
			}
			respond(writer, 200, layout)
			return nil
		}
		if request.Method != "PUT" {
			return domain.NotFound()
		}
		var updated domain.Layout
		if err := readBody(writer, request, &updated); err != nil {
			return err
		}
		layout, err := handler.service.SaveLayout(id, updated)
		if err != nil {
			return err
		}
		respond(writer, 200, layout)
		return nil
	}
	return domain.NotFound()
}
func (handler *Handler) saveApp(writer http.ResponseWriter, request *http.Request, id string) error {
	var body domain.AppSettings
	if err := readBody(writer, request, &body); err != nil {
		return err
	}
	app, secret, err := handler.service.SaveApp(id, body)
	if err != nil {
		return err
	}
	if id == "" {
		respond(writer, 201, map[string]any{"app": app, "secret": secret})
	} else {
		respond(writer, 200, app)
	}
	return nil
}
