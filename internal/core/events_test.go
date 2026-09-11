package core

import (
	"context"
	"testing"
	"time"
)

func TestEventsAreOrderedAndPrunable(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	store.now = func() time.Time { return time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC) }
	first, err := store.RecordEvent(ctx, "worker.started", "wrk-1", "att-1", "ses-1", map[string]string{"source": "test"})
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC) }
	if _, err := store.RecordEvent(ctx, "worker.finished", "wrk-1", "att-1", "ses-1", map[string]string{"status": "ok"}); err != nil {
		t.Fatal(err)
	}
	events, err := store.EventsAfter(ctx, time.Time{}, 10)
	if err != nil || len(events) != 2 || events[0].ID != first.ID {
		t.Fatalf("events=%#v err=%v", events, err)
	}
	recent, err := store.EventsRecent(ctx, 1)
	if err != nil || len(recent) != 1 || recent[0].Kind != "worker.finished" {
		t.Fatalf("recent=%#v err=%v", recent, err)
	}
	deleted, err := store.PruneEvents(ctx, time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC))
	if err != nil || deleted != 1 {
		t.Fatalf("deleted=%d err=%v", deleted, err)
	}
}
