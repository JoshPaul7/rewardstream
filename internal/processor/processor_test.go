package processor

import (
	"context"
	"errors"
	"log/slog"
	"slices"
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

type fakeLake struct {
	err     error
	written []event.Event
}

func (f *fakeLake) WriteBatch(_ context.Context, es []event.Event) error {
	if f.err != nil {
		return f.err
	}
	f.written = append(f.written, es...)
	return nil
}

var now = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

const validBody = `{"event_id":"e-1","type":"click","campaign_id":"cmp-42","user_id":"usr-7","occurred_at":"2026-11-19T11:59:00Z"}`

func newProc(s Store, l Lake) *Processor {
	return &Processor{Store: s, Lake: l, Log: slog.New(slog.DiscardHandler), Now: func() time.Time { return now }}
}

func TestProcessBatch(t *testing.T) {
	tests := []struct {
		name      string
		bodies    []string
		storeErr  error
		lakeErr   error
		want      []outcome
		wantLake  int
		wantStore int
	}{
		{"valid event", []string{validBody}, nil, nil, []outcome{ack}, 1, 1},
		{"malformed json", []string{"not json"}, nil, nil, []outcome{poison}, 0, 0},
		{"invalid event", []string{`{"event_id":"e-2","type":"purchase"}`}, nil, nil, []outcome{poison}, 0, 0},
		{"mixed batch", []string{validBody, "not json"}, nil, nil, []outcome{ack, poison}, 1, 1},
		{"store down", []string{validBody}, errors.New("dynamodb down"), nil, []outcome{retry}, 1, 0},
		{"lake down: nothing recorded", []string{validBody}, nil, errors.New("s3 down"), []outcome{retry}, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{seen: map[string]bool{}, err: tc.storeErr}
			lake := &fakeLake{err: tc.lakeErr}
			got := newProc(store, lake).processBatch(context.Background(), tc.bodies)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("outcomes = %v, want %v", got, tc.want)
			}
			if len(lake.written) != tc.wantLake {
				t.Fatalf("lake got %d events, want %d", len(lake.written), tc.wantLake)
			}
			if len(store.seen) != tc.wantStore {
				t.Fatalf("store recorded %d events, want %d", len(store.seen), tc.wantStore)
			}
		})
	}
}

// Redelivery: counters exactly-once, lake at-least-once.
func TestRedeliveryCountersOnceLakeTwice(t *testing.T) {
	store := &fakeStore{seen: map[string]bool{}}
	lake := &fakeLake{}
	p := newProc(store, lake)
	for i := 0; i < 2; i++ {
		if got := p.processBatch(context.Background(), []string{validBody}); got[0] != ack {
			t.Fatalf("delivery %d: outcome = %v, want ack", i+1, got[0])
		}
	}
	if len(store.seen) != 1 || len(lake.written) != 2 {
		t.Fatalf("store=%d (want 1), lake=%d (want 2)", len(store.seen), len(lake.written))
	}
}
