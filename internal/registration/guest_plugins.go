package registration

import (
	"context"
	"github.com/lanwen/herdr-sbx/internal/guestplugins"
)

// GuestPluginChoices lists defaults without changing any guest or selection.
func (s *Service) GuestPluginChoices(ctx context.Context) ([]guestplugins.Choice, error) {
	return guestplugins.Choices(ctx, s.herdr)
}
