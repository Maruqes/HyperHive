package nots

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sync"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/jwt"
)

var ErrFirebaseUnavailable = errors.New("Firebase backend is not configured")
var ErrFirebaseProject = errors.New("Firebase project does not match backend credentials")
var firebaseProjectPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)

type FirebaseError struct {
	Status  int
	Message string
}

func (e *FirebaseError) Error() string { return e.Message }

type FirebaseSender struct {
	project string
	client  *http.Client
}

var firebaseCache struct {
	sync.Mutex
	digest [32]byte
	sender *FirebaseSender
}

// Reload credentials on requests; reuse the OAuth token source while the file is unchanged.
func NewFirebaseSender() (*FirebaseSender, error) {
	path := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if path == "" {
		return nil, fmt.Errorf("%w: GOOGLE_APPLICATION_CREDENTIALS is not set in the running backend process; configure the backend service environment and restart it", ErrFirebaseUnavailable)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%w: credential file is missing or unreadable by the backend service", ErrFirebaseUnavailable)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(data) > 65536 {
		return nil, ErrFirebaseUnavailable
	}
	var kind map[string]json.RawMessage
	if json.Unmarshal(data, &kind) != nil {
		return nil, fmt.Errorf("%w: invalid credential JSON", ErrFirebaseUnavailable)
	}
	if kind["project_info"] != nil || kind["client"] != nil {
		return nil, fmt.Errorf("%w: google-services.json is an Android client file; the backend needs a service-account JSON from Firebase Project settings > Service accounts", ErrFirebaseUnavailable)
	}
	digest := sha256.Sum256(data)
	firebaseCache.Lock()
	defer firebaseCache.Unlock()
	if firebaseCache.sender != nil && firebaseCache.digest == digest {
		return firebaseCache.sender, nil
	}
	var identity struct {
		Type         string `json:"type"`
		Project      string `json:"project_id"`
		Email        string `json:"client_email"`
		PrivateKey   string `json:"private_key"`
		PrivateKeyID string `json:"private_key_id"`
	}
	if json.Unmarshal(data, &identity) != nil || identity.Type != "service_account" || !firebaseProjectPattern.MatchString(identity.Project) {
		return nil, ErrFirebaseUnavailable
	}
	if identity.Email == "" || identity.PrivateKey == "" {
		return nil, ErrFirebaseUnavailable
	}
	if _, err := jwtgo.ParseRSAPrivateKeyFromPEM([]byte(identity.PrivateKey)); err != nil {
		return nil, ErrFirebaseUnavailable
	}
	cfg := &jwt.Config{Email: identity.Email, PrivateKey: []byte(identity.PrivateKey), PrivateKeyID: identity.PrivateKeyID,
		Scopes: []string{"https://www.googleapis.com/auth/firebase.messaging"}, TokenURL: "https://oauth2.googleapis.com/token"}
	// Token exchange uses its own bounded client, independent of individual request cancellation.
	authContext := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: 10 * time.Second})
	client := cfg.Client(authContext)
	client.Timeout = 15 * time.Second
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	firebaseCache.sender = &FirebaseSender{project: identity.Project, client: client}
	firebaseCache.digest = digest
	return firebaseCache.sender, nil
}

func (s *FirebaseSender) SendTest(ctx context.Context, project, token string) (string, error) {
	return s.SendTypedTest(ctx, project, token, "")
}

func (s *FirebaseSender) SendTypedTest(ctx context.Context, project, token, severity string) (string, error) {
	return s.send(ctx, project, token, false, severity)
}

func (s *FirebaseSender) Check(ctx context.Context, project, token string) error {
	_, err := s.send(ctx, project, token, true, "")
	return err
}

func (s *FirebaseSender) ProjectID() string { return s.project }

func (s *FirebaseSender) send(ctx context.Context, project, token string, validateOnly bool, severity string) (string, error) {
	if s == nil {
		return "", ErrFirebaseUnavailable
	}
	if project != s.project {
		return "", ErrFirebaseProject
	}
	if severity != "" && severity != "info" && severity != "warn" && severity != "error" && severity != "critical" {
		return "", errors.New("invalid Firebase test severity")
	}
	channel := "hyperhive_test_v2"
	title, bodyText, priority := "HyperHive test notification", "Your Android notification test was accepted by Firebase.", "high"
	if severity != "" {
		label := severity
		if label == "warn" {
			label = "warning"
		}
		channel = "hyperhive_test_" + severity
		title = "HyperHive test: " + label
		bodyText = "This is a test " + label + " notification."
		if severity == "info" || severity == "warn" {
			priority = "normal"
		}
	}
	androidNotification := map[string]string{"channel_id": channel, "icon": "hyperhive_notification"}
	if severity == "critical" {
		androidNotification["sound"] = "hyperhive_critical"
	}
	payload := map[string]any{"message": map[string]any{
		"token":        token,
		"notification": map[string]string{"title": title, "body": bodyText},
		"android":      map[string]any{"priority": priority, "notification": androidNotification},
	}}
	if severity != "" {
		payload["message"].(map[string]any)["data"] = map[string]string{"severity": severity, "channelId": channel}
	}
	if validateOnly {
		payload["validate_only"] = true
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://fcm.googleapis.com/v1/projects/"+s.project+"/messages:send", bytes.NewReader(body))
	if err != nil {
		return "", ErrFirebaseUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", &FirebaseError{http.StatusBadGateway, "Firebase request failed or timed out; acceptance is unknown. Check connectivity and backend Firebase credentials before trying again."}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		status, message := http.StatusBadGateway, "Firebase rejected the test notification. Check backend configuration."
		switch resp.StatusCode {
		case 400, 404:
			status, message = http.StatusBadRequest, "Firebase rejected the device token or project. Re-enable notifications and check the Firebase project."
		case 401, 403:
			status, message = http.StatusServiceUnavailable, "Firebase authentication or API access failed. Check backend credentials and enable the FCM API."
		case 429:
			status, message = http.StatusTooManyRequests, "Firebase rate limit reached. Wait before trying again."
		}
		var failure struct {
			Error struct {
				Status  string `json:"status"`
				Details []struct {
					ErrorCode string `json:"errorCode"`
					Reason    string `json:"reason"`
				} `json:"details"`
			} `json:"error"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(&failure) == nil {
			for _, detail := range failure.Error.Details {
				switch detail.ErrorCode {
				case "UNREGISTERED":
					message = "Device token expired or was unregistered. Disable and enable notifications, then check again."
				case "SENDER_ID_MISMATCH":
					message = "Device token belongs to a different Firebase sender. Match the Android client and backend service-account projects."
				}
				if detail.Reason == "SERVICE_DISABLED" {
					message = "Enable the Firebase Cloud Messaging API (fcm.googleapis.com) in the backend credential project."
				}
			}
		}
		return "", &FirebaseError{status, message}
	}
	if validateOnly {
		return "", nil
	}
	var result struct {
		Name string `json:"name"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(&result) != nil || result.Name == "" {
		return "", &FirebaseError{http.StatusBadGateway, "Firebase returned an invalid response; acceptance is unknown."}
	}
	return result.Name, nil
}
