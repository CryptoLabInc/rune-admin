package console

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// UpdateState is the non-sensitive lifecycle surfaced to the console UI.
// Filesystem paths, command output, and backup locations deliberately never
// cross this boundary.
type UpdateState string

const (
	UpdateStateIdle      UpdateState = "idle"
	UpdateStateQueued    UpdateState = "queued"
	UpdateStateRunning   UpdateState = "running"
	UpdateStateFailed    UpdateState = "failed"
	UpdateStateSucceeded UpdateState = "succeeded"
)

var (
	ErrUpdateUnavailable = errors.New("update is unavailable")
	ErrUpdatePending     = errors.New("update is already pending")
	ErrUpdateStale       = errors.New("requested update is stale")
	ErrUpdateInvalid     = errors.New("requested update is invalid")
)

// UpdateStatus is the owner-facing view returned by GET /api/v1/system/update.
// UpdateAvailable means TargetVersion is strictly newer than CurrentVersion;
// Capable additionally proves the separately installed helper is present.
type UpdateStatus struct {
	CurrentVersion  string      `json:"currentVersion"`
	TargetVersion   string      `json:"targetVersion,omitempty"`
	UpdateAvailable bool        `json:"updateAvailable"`
	Capable         bool        `json:"capable"`
	State           UpdateState `json:"state"`
}

// UpdateManager keeps privilege and release discovery outside the console BFF.
// Request must only enqueue a pinned release tag and return before the helper
// stops the daemon.
type UpdateManager interface {
	Check(context.Context) (UpdateStatus, error)
	Request(context.Context, string) error
}

func (s *Service) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	status, err := s.updates.Check(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "UPDATE_CHECK_FAILED", "could not check for updates")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Service) handleUpdateRequest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var body struct {
		Version string `json:"version"`
	}
	if err := decodeUpdateRequest(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "UPDATE_REQUEST_INVALID", "version is required")
		return
	}
	body.Version = strings.TrimSpace(body.Version)
	if body.Version == "" {
		writeError(w, http.StatusBadRequest, "UPDATE_REQUEST_INVALID", "version is required")
		return
	}
	if err := s.updates.Request(r.Context(), body.Version); err != nil {
		switch {
		case errors.Is(err, ErrUpdatePending):
			writeError(w, http.StatusConflict, "UPDATE_ALREADY_PENDING", "an update is already pending")
		case errors.Is(err, ErrUpdateStale), errors.Is(err, ErrUpdateUnavailable):
			writeError(w, http.StatusConflict, "UPDATE_NOT_AVAILABLE", "the requested update is no longer available")
		case errors.Is(err, ErrUpdateInvalid):
			writeError(w, http.StatusBadRequest, "UPDATE_REQUEST_INVALID", "invalid update version")
		default:
			writeError(w, http.StatusServiceUnavailable, "UPDATE_REQUEST_FAILED", "could not schedule the update")
		}
		return
	}
	if s.log != nil {
		s.log.Info("console: update requested", "owner", ownerEmailFromRequest(s, r), "target", body.Version)
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"state": string(UpdateStateQueued), "version": body.Version})
}

func decodeUpdateRequest(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain exactly one JSON object")
	}
	return nil
}

func (s *Service) requireOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner, err := s.owner.get()
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "OWNER_UNAVAILABLE", "console owner could not be verified")
			return
		}
		if owner == nil || owner.Email == "" || owner.Email != ownerEmailFromRequest(s, r) {
			writeError(w, http.StatusForbidden, "OWNER_REQUIRED", "console owner access is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func ownerEmailFromRequest(s *Service, r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	sess := s.sessions.get(c.Value)
	if sess == nil {
		return ""
	}
	return canonicalEmail(emailFromMe(sess.Me))
}
