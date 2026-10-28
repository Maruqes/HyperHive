package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"512SvMan/nots"
)

type fakeFirebaseSender struct {
	calls int
	err   error
}

func (s *fakeFirebaseSender) SendTest(_ context.Context, project, token string) (string, error) {
	s.calls++
	if project != "example-project" || token != "device-one" {
		panic("unexpected payload")
	}
	return "projects/example-project/messages/123", s.err
}
func firebaseRequest(body string, authenticated bool) *http.Request {
	r := httptest.NewRequest("POST", "/notification/firebase/test", strings.NewReader(body))
	if authenticated {
		r = SetTokenInContext(r, "header."+base64.RawURLEncoding.EncodeToString([]byte(`{"attrs":{"id":1}}`))+".signature")
	}
	return r
}
func TestFirebaseHandlerValidationAndCooldown(t *testing.T) {
	sender := &fakeFirebaseSender{}
	handler := newFirebaseTestHandler(sender)
	body := `{"projectId":"example-project","token":"device-one"}`
	for _, tc := range []struct {
		body   string
		auth   bool
		status int
	}{
		{body, false, 401},
		{`{"projectId":"example-project","token":""}`, true, 400},
		{`{"projectId":"example-project","token":"` + strings.Repeat("x", 4097) + `"}`, true, 400},
		{body + ` {}`, true, 400},
		{`{"url":"https://example.com"}`, true, 400},
		{strings.Repeat("x", 8193), true, 400},
		{body + strings.Repeat(" ", 8192), true, 400},
		{body, true, 200},
		{body, true, 429},
	} {
		w := httptest.NewRecorder()
		handler(w, firebaseRequest(tc.body, tc.auth))
		if w.Code != tc.status {
			t.Fatalf("status %d want %d: %s", w.Code, tc.status, w.Body.String())
		}
	}
	if sender.calls != 1 {
		t.Fatal(sender.calls)
	}
}
func TestFirebaseHandlerErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{{nots.ErrFirebaseProject, 409}, {nots.ErrFirebaseUnavailable, 503}, {&nots.FirebaseError{Status: 429, Message: "Wait"}, 429}} {
		handler := newFirebaseTestHandler(&fakeFirebaseSender{err: tc.err})
		w := httptest.NewRecorder()
		handler(w, firebaseRequest(`{"projectId":"example-project","token":"device-one"}`, true))
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
