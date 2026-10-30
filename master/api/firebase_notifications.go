package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"512SvMan/nots"
)

type firebaseTestSender interface {
	SendTest(context.Context, string, string) (string, error)
}

type firebaseTypedTestSender interface {
	SendTypedTest(context.Context, string, string, string) (string, error)
}

// npmUserID reads attrs.id from the NPM token the auth middleware already validated.
func npmUserID(r *http.Request) (int64, bool) {
	parts := strings.Split(GetTokenFromContext(r), ".")
	if len(parts) != 3 {
		return 0, false
	}
	var claims struct {
		Attrs struct {
			ID int64 `json:"id"`
		} `json:"attrs"`
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(payload, &claims) != nil || claims.Attrs.ID <= 0 {
		return 0, false
	}
	return claims.Attrs.ID, true
}

func newFirebaseTestHandler(sender firebaseTestSender, checking bool) http.HandlerFunc {
	cooldown := 30 * time.Second
	if checking {
		cooldown = 5 * time.Second
	}
	var mu sync.Mutex
	recent := make(map[string]time.Time)
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := npmUserID(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var input struct {
			Token     string          `json:"token"`
			ProjectID string          `json:"projectId"`
			Severity  json.RawMessage `json:"severity"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || len(input.Token) < 1 || len(input.Token) > 4096 || strings.ContainsAny(input.Token, " \t\r\n") || len(input.ProjectID) < 6 || len(input.ProjectID) > 30 {
			http.Error(w, "invalid Firebase device token or project ID", http.StatusBadRequest)
			return
		}
		severity := ""
		if len(input.Severity) > 0 && (json.Unmarshal(input.Severity, &severity) != nil || (severity != "info" && severity != "warn" && severity != "error" && severity != "critical")) || (checking && len(input.Severity) > 0) {
			http.Error(w, "invalid Firebase notification severity", http.StatusBadRequest)
			return
		}
		key := strconv.FormatInt(userID, 10)
		now := time.Now()
		mu.Lock()
		for user, last := range recent {
			if now.Sub(last) >= cooldown {
				delete(recent, user)
			}
		}
		if last, exists := recent[key]; exists && now.Sub(last) < cooldown {
			retryAfter := int(cooldown.Seconds() - now.Sub(last).Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}
			mu.Unlock()
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			http.Error(w, "Wait a few seconds before trying again.", http.StatusTooManyRequests)
			return
		}
		recent[key] = now
		mu.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		var name string
		var err error
		if checking {
			checker, ok := sender.(interface {
				Check(context.Context, string, string) error
			})
			if !ok {
				err = nots.ErrFirebaseUnavailable
			} else {
				err = checker.Check(ctx, input.ProjectID, input.Token)
			}
		} else {
			if severity != "" {
				typed, ok := sender.(firebaseTypedTestSender)
				if !ok {
					err = nots.ErrFirebaseUnavailable
				} else {
					name, err = typed.SendTypedTest(ctx, input.ProjectID, input.Token, severity)
				}
			} else {
				name, err = sender.SendTest(ctx, input.ProjectID, input.Token)
			}
		}
		if err != nil {
			status, message := http.StatusBadGateway, "Firebase test failed."
			var providerError *nots.FirebaseError
			switch {
			case errors.Is(err, nots.ErrFirebaseUnavailable):
				status, message = http.StatusServiceUnavailable, err.Error()
			case errors.Is(err, nots.ErrFirebaseProject):
				status, message = http.StatusConflict, nots.ErrFirebaseProject.Error()
			case errors.As(err, &providerError):
				status, message = providerError.Status, providerError.Message
			}
			http.Error(w, message, status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if checking {
			_ = json.NewEncoder(w).Encode(map[string]string{"result": "working", "projectId": input.ProjectID})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "accepted by Firebase", "messageId": name})
	}
}

// Credentials are loaded per request so replacing a mounted credential file does not require a restart.
type liveFirebaseSender struct{}

func (liveFirebaseSender) SendTest(ctx context.Context, project, token string) (string, error) {
	sender, err := nots.NewFirebaseSender()
	if err != nil {
		return "", err
	}
	return sender.SendTest(ctx, project, token)
}
func (liveFirebaseSender) SendTypedTest(ctx context.Context, project, token, severity string) (string, error) {
	sender, err := nots.NewFirebaseSender()
	if err != nil {
		return "", err
	}
	return sender.SendTypedTest(ctx, project, token, severity)
}
func (liveFirebaseSender) Check(ctx context.Context, project, token string) error {
	sender, err := nots.NewFirebaseSender()
	if err != nil {
		return err
	}
	return sender.Check(ctx, project, token)
}
func firebaseConfigurationStatus(w http.ResponseWriter, r *http.Request) {
	sender, err := nots.NewFirebaseSender()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"configured": false, "message": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"configured": true, "projectId": sender.ProjectID(), "message": "Service-account file loaded. Check connection to validate Firebase access."})
}
