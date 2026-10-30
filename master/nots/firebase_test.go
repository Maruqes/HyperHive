package nots

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
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
		if !strings.Contains(string(message["android"]), `"channel_id":"hyperhive_test_v2"`) {
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

func TestFirebaseSeverityPayloadDefaultsAndMappings(t *testing.T) {
	for _, tc := range []struct{ severity, priority string }{{"info", "normal"}, {"warn", "normal"}, {"error", "high"}, {"critical", "high"}} {
		t.Run(tc.severity, func(t *testing.T) {
			label := tc.severity
			if label == "warn" {
				label = "warning"
			}
			sender := &FirebaseSender{project: "example-project", client: &http.Client{Transport: firebaseTransport(func(r *http.Request) (*http.Response, error) {
				var payload struct {
					Message struct {
						Notification map[string]string `json:"notification"`
						Android      struct {
							Priority     string            `json:"priority"`
							Notification map[string]string `json:"notification"`
						} `json:"android"`
						Data map[string]string `json:"data"`
					} `json:"message"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if payload.Message.Android.Priority != tc.priority || payload.Message.Android.Notification["channel_id"] != "hyperhive_test_"+tc.severity || payload.Message.Data["severity"] != tc.severity || payload.Message.Notification["title"] != "HyperHive test: "+label || payload.Message.Notification["body"] != "This is a test "+label+" notification." {
					t.Fatalf("wrong typed payload: %+v", payload.Message)
				}
				if (payload.Message.Android.Notification["sound"] == "hyperhive_critical") != (tc.severity == "critical") {
					t.Fatalf("wrong critical sound: %+v", payload.Message.Android.Notification)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"name":"projects/example-project/messages/123"}`))}, nil
			})}}
			if _, err := sender.SendTypedTest(context.Background(), "example-project", "device-one", tc.severity); err != nil {
				t.Fatal(err)
			}
		})
	}
	sender := &FirebaseSender{project: "example-project"}
	if _, err := sender.SendTypedTest(context.Background(), "example-project", "device-one", "urgent"); err == nil {
		t.Fatal("invalid severity accepted")
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

func TestFirebaseDryRunDoesNotSendNotification(t *testing.T) {
	sender := &FirebaseSender{project: "example-project", client: &http.Client{Transport: firebaseTransport(func(r *http.Request) (*http.Response, error) {
		var body struct {
			ValidateOnly bool `json:"validate_only"`
			Message      struct {
				Token   string `json:"token"`
				Android struct {
					Priority string `json:"priority"`
				} `json:"android"`
			} `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !body.ValidateOnly || body.Message.Token != "device-one" || body.Message.Android.Priority != "high" {
			t.Fatalf("wrong validation payload: %+v", body)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}}
	if err := sender.Check(context.Background(), "example-project", "device-one"); err != nil {
		t.Fatal(err)
	}
}
func TestFirebaseReloadsChangedCredentialsAndRejectsClientFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "firebase.json")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path)
	if err := os.WriteFile(path, []byte(`{"project_info":{},"client":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFirebaseSender(); err == nil || !strings.Contains(err.Error(), "Android client file") {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	credentials := map[string]string{"type": "service_account", "project_id": "example-project", "client_email": "sender@example.invalid", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))}
	write := func() {
		data, _ := json.Marshal(credentials)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	first, err := NewFirebaseSender()
	if err != nil {
		t.Fatal(err)
	}
	again, err := NewFirebaseSender()
	if err != nil || first != again {
		t.Fatal("OAuth source was not reused", err)
	}
	credentials["private_key_id"] = "rotated"
	write()
	changed, err := NewFirebaseSender()
	if err != nil || first == changed {
		t.Fatal("credential rotation was not loaded", err)
	}
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFirebaseSender(); err == nil {
		t.Fatal("invalid credentials reused cached sender")
	}
}
func TestFirebaseReportsProviderReasonWithoutRawDetails(t *testing.T) {
	sender := &FirebaseSender{project: "example-project", client: &http.Client{Transport: firebaseTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"SECRET","details":[{"errorCode":"SENDER_ID_MISMATCH"}]}}`))}, nil
	})}}
	_, err := sender.SendTest(context.Background(), "example-project", "device-one")
	if err == nil || !strings.Contains(err.Error(), "different Firebase sender") || strings.Contains(err.Error(), "SECRET") {
		t.Fatal(err)
	}
}

func TestFirebaseEventPayloadMapsSeverityToChannel(t *testing.T) {
	for _, tc := range []struct {
		severity         Severity
		channel, priorty string
		sound            bool
	}{{SeverityInfo, "hyperhive_test_info", "normal", false}, {SeverityWarning, "hyperhive_test_warn", "normal", false}, {SeverityError, "hyperhive_test_error", "high", false}, {SeverityCritical, "hyperhive_test_critical", "high", true}} {
		sender := &FirebaseSender{project: "example-project", client: &http.Client{Transport: firebaseTransport(func(r *http.Request) (*http.Response, error) {
			var payload struct {
				Message struct {
					Token        string            `json:"token"`
					Notification map[string]string `json:"notification"`
					Android      struct {
						Priority     string            `json:"priority"`
						Notification map[string]string `json:"notification"`
					} `json:"android"`
					Data map[string]string `json:"data"`
				} `json:"message"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			m := payload.Message
			if m.Token != "device" || m.Notification["title"] != "Disk failing" || m.Android.Priority != tc.priorty || m.Android.Notification["channel_id"] != tc.channel || m.Data["event"] != "health.smart" || (m.Android.Notification["sound"] == "hyperhive_critical") != tc.sound {
				t.Fatalf("%s: %+v", tc.severity, m)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"name":"projects/example-project/messages/1"}`))}, nil
		})}}
		if _, err := sender.SendNotification(context.Background(), "device", Notification{Event: "health.smart", Severity: tc.severity, Title: "Disk failing", Body: "b"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFirebaseUnregisteredTokenIsFlagged(t *testing.T) {
	sender := &FirebaseSender{project: "example-project", client: &http.Client{Transport: firebaseTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`))}, nil
	})}}
	_, err := sender.SendNotification(context.Background(), "device", Notification{Title: "t"})
	var providerErr *FirebaseError
	if !errors.As(err, &providerErr) || !providerErr.Unregistered {
		t.Fatalf("unregistered not flagged: %v", err)
	}
}
