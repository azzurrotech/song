package song

import (
	"encoding/json"
	"net/http"
	"time"

	"azzurrotech/song/pkg/auth"
)

// handleAuth routes the magic-link endpoints under /api/auth/*.
func (s *Song) handleAuth(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/auth/generate":
		s.authGenerate(w, r)
	case "/api/auth/validate":
		s.authValidate(w, r)
	case "/api/auth/revoke":
		s.authRevoke(w, r)
	default:
		writeErr(w, http.StatusNotFound, "unknown auth endpoint")
	}
}

func (s *Song) authGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	var req auth.GenerateLinkRequest
	// Accept both JSON and HTML form bodies.
	switch mediaTypeOf(r) {
	case "application/json":
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	default:
		if err := r.ParseForm(); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid form body")
			return
		}
		req.UserID = r.FormValue("user_id")
		req.DeviceInfo = r.FormValue("device_info")
	}
	if req.UserID == "" {
		writeErr(w, http.StatusBadRequest, "user_id is required")
		return
	}
	magicLink, err := s.auth.GenerateMagicLink(req.UserID, req.DeviceInfo)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to generate magic link: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, auth.GenerateLinkResponse{
		MagicLink: magicLink.Token,
		ExpiresAt: magicLink.Expiry,
	})
}

func (s *Song) authValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	var req auth.ValidateLinkRequest
	switch mediaTypeOf(r) {
	case "application/json":
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	default:
		if err := r.ParseForm(); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid form body")
			return
		}
		req.Link = r.FormValue("link")
		req.UserID = r.FormValue("user_id")
		req.DeviceInfo = r.FormValue("device_info")
	}
	if req.Link == "" || req.UserID == "" {
		writeErr(w, http.StatusBadRequest, "link and user_id are required")
		return
	}
	magicLink, err := s.auth.ValidateMagicLink(req.Link, req.UserID, req.DeviceInfo)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid magic link: "+err.Error())
		return
	}
	if magicLink.Used {
		writeErr(w, http.StatusNotFound, "magic link already used")
		return
	}
	if time.Now().After(magicLink.Expiry) {
		_ = s.auth.MarkLinkAsUsed(req.Link)
		writeErr(w, http.StatusNotFound, "magic link expired")
		return
	}
	writeJSON(w, http.StatusOK, auth.ValidateLinkResponse{
		Valid:     true,
		UserID:    magicLink.UserID,
		ExpiresAt: magicLink.Expiry,
	})
}

func (s *Song) authRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	var req struct {
		Link string `json:"link"`
	}
	switch mediaTypeOf(r) {
	case "application/json":
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	default:
		if err := r.ParseForm(); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid form body")
			return
		}
		req.Link = r.FormValue("link")
	}
	if req.Link == "" {
		writeErr(w, http.StatusBadRequest, "link is required")
		return
	}
	if err := s.auth.MarkLinkAsUsed(req.Link); err != nil {
		writeErr(w, http.StatusNotFound, "failed to revoke magic link: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "magic link revoked"})
}

// handleHealth reports service health and basic state.
func (s *Song) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD")
		return
	}
	silos, err := s.store.ListSilos()
	if err != nil {
		silos = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "healthy",
		"service": "song",
		"version": Version,
		"root":    s.store.Root(),
		"silos":   len(silos),
		"time":    time.Now().Format(time.RFC3339),
	})
}
