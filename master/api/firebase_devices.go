package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"512SvMan/db"
)

type firebaseDeviceStore struct {
	save   func(*http.Request, db.FirebaseDevice) error
	delete func(*http.Request, string) error
}

var liveFirebaseDevices = firebaseDeviceStore{
	save:   func(r *http.Request, d db.FirebaseDevice) error { return db.DbSaveFirebaseDevice(r.Context(), d) },
	delete: func(r *http.Request, token string) error { return db.DbDeleteFirebaseDevice(r.Context(), token) },
}

func validFirebaseToken(token string) bool {
	return len(token) >= 1 && len(token) <= 4096 && !strings.ContainsAny(token, " \t\r\n")
}

// decodeStrict decodes one JSON object and rejects unknown fields or trailing data.
func decodeStrict(w http.ResponseWriter, r *http.Request, v any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	return decoder.Decode(v) == nil && decoder.Decode(new(any)) == io.EOF
}

// POST /notification/firebase/devices registers this Android device for real notifications.
// The app calls it after login, on startup and when its FCM token changes.
func (s firebaseDeviceStore) register(w http.ResponseWriter, r *http.Request) {
	userID, ok := npmUserID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var input struct {
		Token     string `json:"token"`
		ProjectID string `json:"projectId"`
	}
	if !decodeStrict(w, r, &input) || !validFirebaseToken(input.Token) || len(input.ProjectID) < 6 || len(input.ProjectID) > 30 {
		http.Error(w, "invalid Firebase device token or project ID", http.StatusBadRequest)
		return
	}
	if err := s.save(r, db.FirebaseDevice{Token: input.Token, ProjectID: input.ProjectID, UserID: userID}); err != nil {
		http.Error(w, "failed to register device", http.StatusInternalServerError)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, map[string]string{"status": "registered"})
}

// POST /notification/firebase/devices/unregister removes this device, e.g. on logout or disable.
func (s firebaseDeviceStore) unregister(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if !decodeStrict(w, r, &input) || !validFirebaseToken(input.Token) {
		http.Error(w, "invalid Firebase device token", http.StatusBadRequest)
		return
	}
	if err := s.delete(r, input.Token); err != nil {
		http.Error(w, "failed to unregister device", http.StatusInternalServerError)
		return
	}
	writeJSONWithStatus(w, http.StatusOK, map[string]string{"status": "unregistered"})
}
