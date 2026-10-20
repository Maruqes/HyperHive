package npmapi

import (
	"512SvMan/db"
	"512SvMan/npm"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

const streamResourceType = "npm_stream"

func listStream(w http.ResponseWriter, r *http.Request) {
	loginToken := GetTokenFromContext(r)

	streams, err := npm.ListStreams(baseURL, loginToken)
	if err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to get Stream hosts: "+err.Error(), http.StatusInternalServerError)
		return
	}
	ids := make([]int, 0, len(streams))
	for _, stream := range streams {
		ids = append(ids, stream.ID)
	}
	descriptions, err := db.GetResourceDescriptions(r.Context(), streamResourceType, ids)
	if err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to get stream descriptions: "+err.Error(), http.StatusInternalServerError)
		return
	}
	for i := range streams {
		streams[i].Description = descriptions[streams[i].ID]
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(streams); err != nil {
		http.Error(w, "failed to marshal streams", http.StatusInternalServerError)
		return
	}
}

func createStream(w http.ResponseWriter, r *http.Request) {
	var p npm.Stream
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)

	id, err := npm.CreateStream(baseURL, loginToken, p)
	if err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to create Stream host: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := db.SetResourceDescription(r.Context(), streamResourceType, id, p.Description); err != nil {
		http.Error(w, "failed to save stream description: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func editStream(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var fields map[string]json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&fields); err != nil {
		http.Error(w, "invalid request body", 400)
		return
	}
	var p npm.Stream
	raw, _ := json.Marshal(fields)
	if json.Unmarshal(raw, &p) != nil || p.ID < 1 {
		http.Error(w, "positive id required", 400)
		return
	}
	loginToken := GetTokenFromContext(r)
	// Existing direct clients use complete replacements. Pool-backed edits merge
	// omitted fields so selecting a reusable target does not erase metadata.
	if _, selected := fields["upstream_id"]; selected {
		existing, err := npm.ListStreams(baseURL, loginToken)
		if err != nil {
			http.Error(w, "failed to read stream", 500)
			return
		}
		found := false
		for _, old := range existing {
			if old.ID != p.ID {
				continue
			}
			found = true
			oldRaw, _ := json.Marshal(old)
			var merged map[string]json.RawMessage
			_ = json.Unmarshal(oldRaw, &merged)
			for key, value := range fields {
				merged[key] = value
			}
			if string(fields["upstream_id"]) != "null" {
				if _, ok := fields["forwarding_host"]; !ok {
					merged["forwarding_host"] = json.RawMessage(`""`)
				}
				if _, ok := fields["forwarding_port"]; !ok {
					merged["forwarding_port"] = json.RawMessage(`0`)
				}
			}
			data, _ := json.Marshal(merged)
			if json.Unmarshal(data, &p) != nil {
				http.Error(w, "invalid request body", 400)
				return
			}
			break
		}
		if !found {
			http.Error(w, "stream not found", 404)
			return
		}
	}

	err := npm.EditStream(baseURL, loginToken, p)
	if err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to edit Stream host: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := db.SetResourceDescription(r.Context(), streamResourceType, p.ID, p.Description); err != nil {
		http.Error(w, "failed to save stream description: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func deleteStream(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	err := npm.DeleteStream(baseURL, loginToken, payload.ID)
	if err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to delete Stream host: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := db.DeleteResourceDescription(r.Context(), streamResourceType, payload.ID); err != nil {
		http.Error(w, "failed to delete stream description: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func disableStream(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	err := npm.DisableStream(baseURL, loginToken, payload.ID)
	if err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to disable Stream host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func enableStream(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	loginToken := GetTokenFromContext(r)
	err := npm.EnableStream(baseURL, loginToken, payload.ID)
	if err != nil {
		var typed *npm.UpstreamError
		if errors.As(err, &typed) {
			upstreamFailure(w, err)
			return
		}
		http.Error(w, "failed to enable Stream host: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func SetupStreamAPI(r chi.Router) chi.Router {
	return r.Route("/stream", func(r chi.Router) {
		r.Get("/list", listStream)
		r.Post("/create", createStream)
		r.Put("/edit", editStream)
		r.Delete("/delete", deleteStream)
		r.Post("/disable", disableStream)
		r.Post("/enable", enableStream)
	})
}
