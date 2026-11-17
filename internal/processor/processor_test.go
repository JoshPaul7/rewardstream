package processor

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/JoshPaul7/rewardstream/internal/event"
)

type fakeStore struct {
	seen map[string]bool
	err  error
}

func (f *fakeStore) Record(_ context.Context, e event.Event) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	if f.seen[e.ID] {
		return true, nil
	}
	f.seen[e.ID] = true
	return false, nil
}

var now = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

const validBody = `{"event_id":"e-1","type":"click","campaign_id":"cmp-42","user_id":"usr-7","occurred_at":"2026-11-18T11:59:00Z"}`

func newProc(s Store) *Processor {
	return &Processor{Store: s, Log: slog.New(slog.DiscardHandler), Now: func() time.Time { return now }}
}

func TestHandle(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		storeErr error
		want     outcome
	}{
		{"valid event", validBody, nil, ack},
		{"malformed json", "not json", nil, poison},
		{"invalid event", `{"event_id":"e-2","type":"purchase"}`, nil, poison},
		{"store down", validBody, errors.New("dynamodb unavailable"), retry},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newProc(&fakeStore{seen: map[string]bool{}, err: tc.storeErr})
			if got := p.handle(context.Background(), tc.body); got != tc.want {
				t.Fatalf("outcome = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestHandleDuplicateIsAckedAndRecordedOnce(t *testing.T) {
	store := &fakeStore{seen: map[string]bool{}}
	p := newProc(store)
	for i := 0; i < 2; i++ {
		if got := p.handle(context.Background(), validBody); got != ack {
			t.Fatalf("delivery %d: outcome = %d, want ack", i+1, got)
		}
	}
	if len(store.seen) != 1 {
		t.Fatalf("recorded %d events, want 1", len(store.seen))
	}
}
