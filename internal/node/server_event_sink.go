package node

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
)

// NewStoreEventSink is the production acceptance boundary for Node events.
// It writes activity to the durable event log and terminal outcomes through
// the lifecycle transaction before the protocol ACK is emitted.
func NewStoreEventSink(store *core.Store) func(context.Context, NodeEvent) error {
	return func(ctx context.Context, event NodeEvent) error {
		if store == nil {
			return errors.New("node server: durable event store is required")
		}
		if err := event.Validate(); err != nil {
			return err
		}
		if event.Outcome != nil {
			if event.Kind != "attempt.outcome" {
				return fmt.Errorf("node server: invalid terminal event kind %q", event.Kind)
			}
			_, _, _, err := store.RecordNodeAttemptOutcome(ctx, *event.Outcome)
			return err
		}
		if event.Kind != "activity" {
			return fmt.Errorf("node server: invalid activity event kind %q", event.Kind)
		}
		record, err := store.NodeRecord(ctx, event.Node)
		if err != nil {
			return err
		}
		for _, instance := range record.Inventory.Instances {
			if instance.ID == event.Activity.Metadata.HarnessInstanceID {
				_, err := store.RecordNodeActivity(ctx, instance, *event.Activity)
				return err
			}
		}
		return fmt.Errorf("node server: HarnessInstance %q is not in observed Node inventory", strings.TrimSpace(string(event.Activity.Metadata.HarnessInstanceID)))
	}
}
