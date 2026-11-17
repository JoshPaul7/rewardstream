package event

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

func validEvent() Event {
	return Event{
		ID:         "6f1c2b9e-3d4a-4e8b-9c1d-2a7f5e8b0c13",
		Type:       TypeClick,
		CampaignID: "cmp-42",
		UserID:     "usr-7",
		OccurredAt: now.Add(-time.Minute),
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(e *Event)
		wantErr bool
	}{
		{"valid click", func(e *Event) {}, false},
		{"valid impression", func(e *Event) { e.Type = TypeImpression }, false},
		{"valid install", func(e *Event) { e.Type = TypeInstall }, false},
		{"valid reward", func(e *Event) { e.Type = TypeRewardGranted; e.RewardAmount = 50 }, false},
		{"empty event_id", func(e *Event) { e.ID = "" }, true},
		{"event_id at max length", func(e *Event) { e.ID = strings.Repeat("a", 64) }, false},
		{"event_id too long", func(e *Event) { e.ID = strings.Repeat("a", 65) }, true},
		{"unknown type", func(e *Event) { e.Type = "purchase" }, true},
		{"missing campaign_id", func(e *Event) { e.CampaignID = "" }, true},
		{"missing user_id", func(e *Event) { e.UserID = "" }, true},
		{"zero occurred_at", func(e *Event) { e.OccurredAt = time.Time{} }, true},
		{"exactly max skew", func(e *Event) { e.OccurredAt = now.Add(MaxClockSkew) }, false},
		{"beyond max skew", func(e *Event) { e.OccurredAt = now.Add(MaxClockSkew + time.Nanosecond) }, true},
		{"reward without amount", func(e *Event) { e.Type = TypeRewardGranted }, true},
		{"click with amount", func(e *Event) { e.RewardAmount = 10 }, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := validEvent()
			tc.mutate(&e)
			err := e.Validate(now)
			if tc.wantErr && !errors.Is(err, ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("want nil, got %v", err)
			}
		})
	}
}

func TestJSONFieldNames(t *testing.T) {
	b, err := json.Marshal(validEvent())
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, k := range []string{`"event_id"`, `"type"`, `"campaign_id"`, `"user_id"`, `"occurred_at"`} {
		if !strings.Contains(s, k) {
			t.Errorf("missing %s in %s", k, s)
		}
	}
	if strings.Contains(s, "reward_amount") {
		t.Errorf("reward_amount should be omitted when zero: %s", s)
	}
}
