package nots

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/jwt"
)

var ErrFirebaseUnavailable = errors.New("Firebase sender unavailable; configure GOOGLE_APPLICATION_CREDENTIALS with a valid service-account file and restart HyperHive")
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

// NewFirebaseSender reads only server-side service-account credentials. Failure disables this feature.
func NewFirebaseSender() (*FirebaseSender, error) {
	path := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if path == "" {
		return nil, ErrFirebaseUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrFirebaseUnavailable
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(data) > 65536 {
		return nil, ErrFirebaseUnavailable
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
	return &FirebaseSender{project: identity.Project, client: client}, nil
}

func (s *FirebaseSender) SendTest(ctx context.Context, project, token string) (string, error) {
	if s == nil {
		return "", ErrFirebaseUnavailable
	}
	if project != s.project {
		return "", ErrFirebaseProject
	}
	payload := map[string]any{"message": map[string]any{
		"token":        token,
		"notification": map[string]string{"title": "HyperHive test notification", "body": "Your Android notification test was accepted by Firebase."},
		"android":      map[string]any{"notification": map[string]string{"channel_id": "hyperhive_test", "icon": "hyperhive_notification"}},
	}}
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
		return "", &FirebaseError{status, message}
	}
	var result struct {
		Name string `json:"name"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(&result) != nil || result.Name == "" {
		return "", &FirebaseError{http.StatusBadGateway, "Firebase returned an invalid response; acceptance is unknown."}
	}
	return result.Name, nil
}
