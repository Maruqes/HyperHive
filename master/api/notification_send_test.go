package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"512SvMan/nots"
)

func TestSendNotification(t *testing.T) {
	var got []nots.Notification
	sender := notificationSender{notify: func(_ context.Context, n nots.Notification) error {
		got = append(got, n)
		return nil
	}}
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"severity":"critical","message":"disk full"}`, 202},
		{`{"severity":"warn","message":"cpu hot","title":"Backup","event":"Backup.Nightly","url":"/automations","dedup_key":"k1"}`, 202},
		{`{"severity":"info","message":"x","event":"custom.mine"}`, 202},
		{`{"severity":"loud","message":"x"}`, 400},
		{`{"severity":"info","message":"  "}`, 400},
		{`{"severity":"info","message":"x","url":"https://evil.example"}`, 400},
		{`{"severity":"info","message":"x","url":"//evil.example"}`, 400},
		{`{"severity":"info","message":"x","event":"bad event"}`, 400},
		{`{"severity":"info","message":"` + strings.Repeat("a", 4001) + `"}`, 400},
		{`{"severity":"info","message":"x","extra":1}`, 400},
	} {
		w := httptest.NewRecorder()
		sender.send(w, httptest.NewRequest(http.MethodPost, "/notification/send", strings.NewReader(tc.body)))
		if w.Code != tc.status {
			t.Fatalf("%.80s: got %d want %d (%s)", tc.body, w.Code, tc.status, w.Body.String())
		}
	}
	if len(got) != 3 {
		t.Fatalf("notifications: %+v", got)
	}
	if got[0].Event != "custom.api" || got[0].Severity != nots.SeverityCritical || got[0].Body != "disk full" || got[0].DedupKey != "" {
		t.Fatalf("first: %+v", got[0])
	}
	if got[1].Event != "custom.backup.nightly" || got[1].Severity != nots.SeverityWarning || got[1].Title != "Backup" || got[1].URL != "/automations" || got[1].DedupKey != "custom:k1" {
		t.Fatalf("second: %+v", got[1])
	}
	if got[2].Event != "custom.mine" || got[2].Category() != nots.CategoryCustom {
		t.Fatalf("third: %+v", got[2])
	}
}
