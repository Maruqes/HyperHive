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

func newFirebaseTestHandler(sender firebaseTestSender) http.HandlerFunc {
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
			if now.Sub(last) >= 30*time.Second {
				delete(recent, user)
			}
		}
		if last, exists := recent[key]; exists && now.Sub(last) < 30*time.Second {
			mu.Unlock()
			w.Header().Set("Retry-After", "30")
			http.Error(w, "Wait 30 seconds before sending another test.", http.StatusTooManyRequests)
			return
		}
		recent[key] = now
		mu.Unlock()
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		name, err := sender.SendTest(ctx, input.ProjectID, input.Token)
		if err != nil {
			status, message := http.StatusBadGateway, "Firebase test failed."
			var providerError *nots.FirebaseError
			switch {
			case errors.Is(err, nots.ErrFirebaseUnavailable):
				status, message = http.StatusServiceUnavailable, nots.ErrFirebaseUnavailable.Error()
			case errors.Is(err, nots.ErrFirebaseProject):
				status, message = http.StatusConflict, nots.ErrFirebaseProject.Error()
			case errors.As(err, &providerError):
				status, message = providerError.Status, providerError.Message
			}
			http.Error(w, message, status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"result": "accepted by Firebase", "messageId": name})
	}
}
