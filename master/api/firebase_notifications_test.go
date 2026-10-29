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
	calls      int
	checks     int
	err        error
	severities []string
}

func (s *fakeFirebaseSender) SendTypedTest(_ context.Context, project, token, severity string) (string, error) {
	s.severities = append(s.severities, severity)
	return s.SendTest(context.Background(), project, token)
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
	handler := newFirebaseTestHandler(sender, false)
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
		handler := newFirebaseTestHandler(&fakeFirebaseSender{err: tc.err}, false)
		w := httptest.NewRecorder()
		handler(w, firebaseRequest(`{"projectId":"example-project","token":"device-one"}`, true))
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func (s *fakeFirebaseSender) Check(_ context.Context, project, token string) error {
	s.checks++
	return s.err
}
func TestFirebaseCheckValidatesWithoutSending(t *testing.T) {
	sender := &fakeFirebaseSender{}
	handler := newFirebaseTestHandler(sender, true)
	w := httptest.NewRecorder()
	handler(w, firebaseRequest(`{"projectId":"example-project","token":"device-one"}`, true))
	if w.Code != 200 || sender.calls != 0 || sender.checks != 1 || !strings.Contains(w.Body.String(), `"result":"working"`) {
		t.Fatal(w.Code, w.Body.String(), sender)
	}
}

func TestFirebaseTypedTestsValidateAndShareCooldown(t *testing.T) {
	var severities []string
	for _, severity := range []string{"info", "warn", "error", "critical"} {
		sender := &fakeFirebaseSender{}
		handler := newFirebaseTestHandler(sender, false)
		w := httptest.NewRecorder()
		handler(w, firebaseRequest(`{"projectId":"example-project","token":"device-one","severity":"`+severity+`"}`, true))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", severity, w.Code, w.Body.String())
		}
		severities = append(severities, sender.severities...)
	}
	if strings.Join(severities, ",") != "info,warn,error,critical" {
		t.Fatal(severities)
	}
	sender := &fakeFirebaseSender{}
	handler := newFirebaseTestHandler(sender, false)
	first := httptest.NewRecorder()
	handler(first, firebaseRequest(`{"projectId":"example-project","token":"device-one","severity":"info"}`, true))
	second := httptest.NewRecorder()
	handler(second, firebaseRequest(`{"projectId":"example-project","token":"device-one","severity":"critical"}`, true))
	if first.Code != 200 || second.Code != 429 || sender.calls != 1 || second.Header().Get("Retry-After") == "" {
		t.Fatal(first.Code, second.Code, sender.calls)
	}
	for _, rawSeverity := range []string{`"urgent"`, `""`, `null`, `1`} {
		bad := httptest.NewRecorder()
		newFirebaseTestHandler(&fakeFirebaseSender{}, false)(bad, firebaseRequest(`{"projectId":"example-project","token":"device-one","severity":`+rawSeverity+`}`, true))
		if bad.Code != 400 {
			t.Fatalf("severity %s: %d", rawSeverity, bad.Code)
		}
	}
}

func TestFirebaseCheckRejectsSeverity(t *testing.T) {
	sender := &fakeFirebaseSender{}
	for _, rawSeverity := range []string{`"info"`, `null`} {
		w := httptest.NewRecorder()
		newFirebaseTestHandler(sender, true)(w, firebaseRequest(`{"projectId":"example-project","token":"device-one","severity":`+rawSeverity+`}`, true))
		if w.Code != 400 || sender.checks != 0 || sender.calls != 0 {
			t.Fatal(w.Code, sender)
		}
	}
}
func TestFirebaseStatusExplainsMissingCredentialEnvironment(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	w := httptest.NewRecorder()
	firebaseConfigurationStatus(w, firebaseRequest("", true))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"configured":false`) || !strings.Contains(w.Body.String(), "GOOGLE_APPLICATION_CREDENTIALS") {
		t.Fatal(w.Code, w.Body.String())
	}
}
