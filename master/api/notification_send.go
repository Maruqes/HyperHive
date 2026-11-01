package api

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"512SvMan/nots"
)

const (
	maxSendTitle   = 300
	maxSendMessage = 4000
	maxSendEvent   = 100
	maxSendURL     = 500
	maxSendDedup   = 200
)

var sendEventPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)

type notificationSender struct {
	notify func(context.Context, nots.Notification) error
}

var liveNotificationSender = notificationSender{notify: nots.Notify}

// POST /notification/send stores a custom notification and delivers it through
// the rules of the "custom" category. Body:
// { severity, message, title?, event?, url?, dedup_key? }.
// It returns 202 once history is written; channel delivery runs in the background.
func (s notificationSender) send(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Severity string `json:"severity"`
		Message  string `json:"message"`
		Title    string `json:"title"`
		Event    string `json:"event"`
		URL      string `json:"url"`
		DedupKey string `json:"dedup_key"`
	}
	if !decodeStrict(w, r, &input) {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	n, err := customNotification(input.Severity, input.Message, input.Title, input.Event, input.URL, input.DedupKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.notify(r.Context(), n); err != nil {
		http.Error(w, "failed to save notification", http.StatusInternalServerError)
		return
	}
	writeJSONWithStatus(w, http.StatusAccepted, map[string]string{"status": "accepted", "event": n.Event, "severity": n.Severity.String()})
}

type sendValidationError string

func (e sendValidationError) Error() string { return string(e) }

// customNotification validates the request and keeps the event inside the custom category.
func customNotification(severity, message, title, event, url, dedupKey string) (nots.Notification, error) {
	level, err := nots.ParseSeverity(severity)
	if err != nil {
		return nots.Notification{}, err
	}
	message, title, event, url, dedupKey = strings.TrimSpace(message), strings.TrimSpace(title), strings.TrimSpace(event), strings.TrimSpace(url), strings.TrimSpace(dedupKey)
	switch {
	case message == "":
		return nots.Notification{}, sendValidationError("message is required")
	case utf8.RuneCountInString(message) > maxSendMessage:
		return nots.Notification{}, sendValidationError("message is longer than 4000 characters")
	case utf8.RuneCountInString(title) > maxSendTitle:
		return nots.Notification{}, sendValidationError("title is longer than 300 characters")
	case len(dedupKey) > maxSendDedup:
		return nots.Notification{}, sendValidationError("dedup_key is longer than 200 characters")
	case len(url) > maxSendURL || (url != "" && (!strings.HasPrefix(url, "/") || strings.HasPrefix(url, "//"))):
		return nots.Notification{}, sendValidationError("url must be a relative path starting with /")
	}
	event = strings.TrimPrefix(strings.ToLower(event), nots.CategoryCustom+".")
	if event == "" {
		event = "api"
	}
	if len(event) > maxSendEvent || !sendEventPattern.MatchString(event) {
		return nots.Notification{}, sendValidationError("event may only contain a-z, 0-9, '.', '_' and '-' (max 100)")
	}
	n := nots.Notification{
		Event:    nots.CategoryCustom + "." + event,
		Severity: level,
		Title:    title,
		Body:     message,
		URL:      url,
	}
	if dedupKey != "" {
		// Scope dedup keys so custom senders cannot suppress built-in notifications.
		n.DedupKey = nots.CategoryCustom + ":" + dedupKey
	}
	return n, nil
}
