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

func newFirebaseTestHandler(sender firebaseTestSender, checking bool) http.HandlerFunc {
	cooldown := 30 * time.Second
	if checking {
		cooldown = 5 * time.Second
	}
	var mu sync.Mutex
	recent := make(map[string]time.Time)
	return func(w http.ResponseWriter, r *http.Request) {
		// Middleware already authenticated the NPM token; attrs.id identifies its user across sessions.
		parts := strings.Split(GetTokenFromContext(r), ".")
		var claims struct {
			Attrs struct {
				ID int64 `json:"id"`
			} `json:"attrs"`
		}
		if len(parts) != 3 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil || json.Unmarshal(payload, &claims) != nil || claims.Attrs.ID <= 0 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var input struct {
			Token     string `json:"token"`
			ProjectID string `json:"projectId"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || len(input.Token) < 1 || len(input.Token) > 4096 || strings.ContainsAny(input.Token, " \t\r\n") || len(input.ProjectID) < 6 || len(input.ProjectID) > 30 {
			http.Error(w, "invalid Firebase device token or project ID", http.StatusBadRequest)
			return
		}
		key := strconv.FormatInt(claims.Attrs.ID, 10)
		now := time.Now()
		mu.Lock()
		for user, last := range recent {
			if now.Sub(last) >= cooldown {
				delete(recent, user)
			}
		}
		if last, exists := recent[key]; exists && now.Sub(last) < cooldown {
			mu.Unlock()
			w.Header().Set("Retry-After", strconv.Itoa(int(cooldown.Seconds())))
			http.Error(w, "Wait a few seconds before trying again.", http.StatusTooManyRequests)
			return
		}
		recent[key] = now
		mu.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		var name string
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
			name, err = sender.SendTest(ctx, input.ProjectID, input.Token)
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
