package ingest

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JoshPaul7/rewardstream/internal/event"
)

type fakePublisher struct {
	err       error
	published []event.Event
}

func (f *fakePublisher) Publish(_ context.Context, e event.Event) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, e)
	return nil
}

var now = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

const validBody = `{"event_id":"e-1","type":"click","campaign_id":"cmp-42","user_id":"usr-7","occurred_at":"2026-11-17T11:59:00Z"}`

func TestHandler(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		pubErr     error
		wantStatus int
		wantPub    int
	}{
		{"valid event", validBody, nil, http.StatusAccepted, 1},
		{"invalid event", strings.Replace(validBody, `"click"`, `"purchase"`, 1), nil, http.StatusBadRequest, 0},
		{"malformed json", `{"event_id":`, nil, http.StatusBadRequest, 0},
		{"unknown field", strings.Replace(validBody, `}`, `,"extra":1}`, 1), nil, http.StatusBadRequest, 0},
		{"body too large", `{"event_id":"` + strings.Repeat("a", 70<<10) + `"}`, nil, http.StatusRequestEntityTooLarge, 0},
		{"publisher down", validBody, errors.New("sns unavailable"), http.StatusServiceUnavailable, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{err: tc.pubErr}
			h := &Handler{Pub: pub, Log: slog.New(slog.DiscardHandler), Now: func() time.Time { return now }}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/events", strings.NewReader(tc.body)))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tc.wantStatus, rec.Body)
			}
			if len(pub.published) != tc.wantPub {
				t.Fatalf("published %d events, want %d", len(pub.published), tc.wantPub)
			}
		})
	}
}
