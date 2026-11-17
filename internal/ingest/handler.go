// Package ingest implements the HTTP API that accepts ad events.
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/JoshPaul7/rewardstream/internal/event"
)

// Publisher sends a validated event to the messaging backend. It is defined here,
// where it is used; SNS (and later Kafka) implement it in their own packages.
type Publisher interface {
	Publish(ctx context.Context, e event.Event) error
}

const (
	maxBodyBytes   = 64 << 10 // 64 KiB; one event is well under 1 KiB
	publishTimeout = 3 * time.Second
)

// Handler serves POST /v1/events.
type Handler struct {
	Pub Publisher
	Log *slog.Logger
	Now func() time.Time
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var e event.Event
	if err := dec.Decode(&e); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "malformed JSON: "+err.Error())
		return
	}

	if err := e.Validate(h.Now()); err != nil {
		if errors.Is(err, event.ErrInvalid) { // works because Validate wraps with %w
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), publishTimeout)
	defer cancel()
	if err := h.Pub.Publish(ctx, e); err != nil {
		h.Log.ErrorContext(ctx, "publish failed", "event_id", e.ID, "err", err) // never log the full event
		writeError(w, http.StatusServiceUnavailable, "temporarily unable to accept events")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"event_id": e.ID})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
