package npmapi

import (
	"512SvMan/npm"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func upstreamJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status != http.StatusNoContent {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func upstreamFailure(w http.ResponseWriter, err error) {
	status, body := npm.UpstreamErrorDetails(err)
	upstreamJSON(w, status, body)
}

func upstreamID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		return 0, &npm.UpstreamError{Status: 400, Code: "invalid_upstream_id"}
	}
	return id, nil
}

func decodeUpstream(w http.ResponseWriter, r *http.Request, p *npm.Upstream) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	defer r.Body.Close()
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(p); err != nil {
		upstreamJSON(w, 400, map[string]string{"error": "invalid_upstream_body"})
		return false
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		upstreamJSON(w, 400, map[string]string{"error": "invalid_upstream_body"})
		return false
	}
	return true
}

func listUpstreams(w http.ResponseWriter, r *http.Request) {
	ps, err := npm.ListUpstreams(r.Context())
	if err != nil {
		upstreamFailure(w, err)
		return
	}
	upstreamJSON(w, 200, ps)
}
func getUpstream(w http.ResponseWriter, r *http.Request) {
	id, err := upstreamID(r)
	if err != nil {
		upstreamFailure(w, err)
		return
	}
	p, err := npm.GetUpstream(r.Context(), id)
	if err != nil {
		upstreamFailure(w, err)
		return
	}
	w.Header().Set("ETag", `"`+strconv.FormatInt(p.Revision, 10)+`"`)
	upstreamJSON(w, 200, p)
}
func createUpstream(w http.ResponseWriter, r *http.Request) {
	var p npm.Upstream
	if !decodeUpstream(w, r, &p) {
		return
	}
	if p.ID != 0 || p.Revision != 0 {
		upstreamJSON(w, 400, map[string]string{"error": "create_requires_no_id_or_revision"})
		return
	}
	p, err := npm.SaveUpstream(npm.WithUpstreamNPMContext(r.Context(), baseURL, GetTokenFromContext(r)), p, 0)
	if err != nil {
		upstreamFailure(w, err)
		return
	}
	w.Header().Set("Location", "/upstream/"+strconv.FormatInt(p.ID, 10))
	upstreamJSON(w, 201, p)
}
func editUpstream(w http.ResponseWriter, r *http.Request) {
	id, err := upstreamID(r)
	if err != nil {
		upstreamFailure(w, err)
		return
	}
	var p npm.Upstream
	if !decodeUpstream(w, r, &p) {
		return
	}
	if p.ID != 0 && p.ID != id || p.Revision < 1 {
		upstreamJSON(w, 400, map[string]string{"error": "matching_id_and_expected_revision_required"})
		return
	}
	p.ID = id
	p, err = npm.SaveUpstream(npm.WithUpstreamNPMContext(r.Context(), baseURL, GetTokenFromContext(r)), p, p.Revision)
	if err != nil {
		upstreamFailure(w, err)
		return
	}
	upstreamJSON(w, 200, p)
}
func deleteUpstream(w http.ResponseWriter, r *http.Request) {
	id, err := upstreamID(r)
	if err != nil {
		upstreamFailure(w, err)
		return
	}
	revision, err := strconv.ParseInt(strings.Trim(r.Header.Get("If-Match"), `"`), 10, 64)
	if err != nil || revision < 1 {
		upstreamJSON(w, 400, map[string]string{"error": "If-Match_revision_required"})
		return
	}
	if err := npm.DeleteUpstream(npm.WithUpstreamNPMContext(r.Context(), baseURL, GetTokenFromContext(r)), id, revision); err != nil {
		upstreamFailure(w, err)
		return
	}
	upstreamJSON(w, 204, nil)
}
func upstreamCapabilities(w http.ResponseWriter, r *http.Request) {
	caps, err := npm.UpstreamCapabilities(r.Context())
	if err != nil {
		upstreamFailure(w, err)
		return
	}
	upstreamJSON(w, 200, caps)
}

// SetupUpstreamAPI must be mounted in the existing authenticated admin router.
func SetupUpstreamAPI(r chi.Router) chi.Router {
	return r.Route("/upstream", func(r chi.Router) {
		r.Get("/list", listUpstreams)
		r.Get("/capabilities", upstreamCapabilities)
		r.Post("/create", createUpstream)
		r.Get("/{id}", getUpstream)
		r.Put("/{id}", editUpstream)
		r.Delete("/{id}", deleteUpstream)
	})
}
