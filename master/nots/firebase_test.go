package nots

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type firebaseTransport func(*http.Request) (*http.Response, error)

func (f firebaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFirebaseTargetsOneTokenAndRejectsMismatch(t *testing.T) {
	calls := 0
	sender := &FirebaseSender{project: "example-project", client: &http.Client{Transport: firebaseTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://fcm.googleapis.com/v1/projects/example-project/messages:send" {
			t.Fatal(r.URL)
		}
		var payload map[string]map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		message := payload["message"]
		if string(message["token"]) != `"device-one"` || message["topic"] != nil || message["condition"] != nil {
			t.Fatalf("unexpected targets: %s", message)
		}
		if string(message["notification"]) != `{"body":"Your Android notification test was accepted by Firebase.","title":"HyperHive test notification"}` {
			t.Fatal(string(message["notification"]))
		}
		if !strings.Contains(string(message["android"]), `"channel_id":"hyperhive_test"`) {
			t.Fatal("missing channel")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"name":"projects/example-project/messages/123"}`))}, nil
	})}}
	if _, err := sender.SendTest(context.Background(), "other-project", "device-one"); !errors.Is(err, ErrFirebaseProject) || calls != 0 {
		t.Fatal("mismatch sent")
	}
	if name, err := sender.SendTest(context.Background(), "example-project", "device-one"); err != nil || name == "" || calls != 1 {
		t.Fatalf("%q %v calls=%d", name, err, calls)
	}
}

func TestFirebaseSanitizesProviderFailures(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 429, 500} {
		sender := &FirebaseSender{project: "example-project", client: &http.Client{Transport: firebaseTransport(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("private_key=SECRET device-token=SECRET"))}, nil
		})}}
		_, err := sender.SendTest(context.Background(), "example-project", "device")
		if err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("unsanitized %d: %v", status, err)
		}
	}
	var sender *FirebaseSender
	if _, err := sender.SendTest(context.Background(), "example-project", "device"); !errors.Is(err, ErrFirebaseUnavailable) {
		t.Fatal(err)
	}
}

func TestFirebaseMissingCredentials(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	if _, err := NewFirebaseSender(); !errors.Is(err, ErrFirebaseUnavailable) {
		t.Fatal(err)
	}
}

func TestFirebaseRejectsInvalidCredentialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte(`{"type":"service_account","project_id":"example-project","client_email":"service@example.invalid","private_key":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path)
	if _, err := NewFirebaseSender(); !errors.Is(err, ErrFirebaseUnavailable) {
		t.Fatal(err)
	}
}
