package httpapi

import (
	"net/http"

	"hyperhive/support/internal/domain"
)

func (handler *Handler) automationsRoute(writer http.ResponseWriter, request *http.Request, parts []string) error {
	if len(parts) == 1 {
		switch request.Method {
		case "GET":
			list, err := handler.service.Automations()
			if err != nil {
				return err
			}
			respond(writer, 200, list)
			return nil
		case "POST":
			var body domain.AutomationInput
			if err := readBody(writer, request, &body); err != nil {
				return err
			}
			automation, err := handler.service.CreateAutomation(body)
			if err != nil {
				return err
			}
			respond(writer, 201, automation)
			return nil
		}
		return domain.NotFound()
	}
	if len(parts) != 2 {
		return domain.NotFound()
	}
	switch {
	case parts[1] == "settings" && request.Method == "GET":
		respond(writer, 200, handler.service.AutomationSettings())
		return nil
	case parts[1] == "settings" && request.Method == "PUT":
		var body domain.AutomationSettings
		if err := readBody(writer, request, &body); err != nil {
			return err
		}
		settings, err := handler.service.SaveAutomationSettings(body)
		if err != nil {
			return err
		}
		respond(writer, 200, settings)
		return nil
	case parts[1] == "evaluate" && request.Method == "POST":
		var body struct {
			Condition string `json:"condition"`
		}
		if err := readBody(writer, request, &body); err != nil {
			return err
		}
		result, err := handler.service.TestCondition(body.Condition)
		if err != nil {
			return err
		}
		respond(writer, 200, result)
		return nil
	}
	id := parts[1]
	switch request.Method {
	case "GET":
		automation, err := handler.service.Automation(id)
		if err != nil {
			return err
		}
		respond(writer, 200, automation)
		return nil
	case "PUT":
		var body domain.AutomationInput
		if err := readBody(writer, request, &body); err != nil {
			return err
		}
		automation, err := handler.service.UpdateAutomation(id, body)
		if err != nil {
			return err
		}
		respond(writer, 200, automation)
		return nil
	case "DELETE":
		if err := handler.service.DeleteAutomation(id); err != nil {
			return err
		}
		respond(writer, 204, nil)
		return nil
	}
	return domain.NotFound()
}
