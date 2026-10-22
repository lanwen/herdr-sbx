package registration

import (
	"context"
	"github.com/lanwen/herdr-sbx/internal/store"
)

type explicitActionKey struct{}

// WithExplicitAction permits only the requested operation while Management is OFF.
func WithExplicitAction(ctx context.Context) context.Context {
	return context.WithValue(ctx, explicitActionKey{}, true)
}

// CheckAutomaticWork rejects further automatic steps as soon as OFF is saved.
func CheckAutomaticWork(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if explicit, _ := ctx.Value(explicitActionKey{}).(bool); explicit {
		return nil
	}
	saved, err := store.ReadStatusStore()
	if err != nil {
		return err
	}
	if saved.WorkerControl != nil && saved.WorkerControl.Stopped {
		return context.Canceled
	}
	return nil
}

func sandboxWorkAllowed(ctx context.Context, scope, id string) bool {
	if CheckAutomaticWork(ctx) != nil {
		return false
	}
	status, err := store.ReadStatusStore()
	key := store.StatusKey(scope, id)
	return err == nil && status.EnabledChoices[key] != store.Disabled && !status.EnabledObservationPending[key]
}
