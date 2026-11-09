package subscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/A1nar-S/advertisiment-monitoring/internal/events"
	"github.com/A1nar-S/advertisiment-monitoring/internal/subscriptions/subscriptionsdb"
)

// ErrNotAdmin: only the bot's admins may change service-wide settings.
var ErrNotAdmin = errors.New("not an admin")

const statusChannelKey = "status_channel"

// StatusChannel returns where the service's start/stop/error messages go,
// as set with SetStatusChannel. ok is false when it was never set (the .env
// setting applies then); an empty Target means they were turned off.
func (s *Service) StatusChannel(ctx context.Context) (ch events.Channel, ok bool, err error) {
	raw, err := s.q.GetSetting(ctx, statusChannelKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return ch, false, nil
	}
	if err != nil {
		return ch, false, fmt.Errorf("status channel: %w", err)
	}
	if err := json.Unmarshal(raw, &ch); err != nil {
		return ch, false, fmt.Errorf("status channel: %w", err)
	}
	return ch, true, nil
}

// SetStatusChannel sends status messages to ch, or nowhere when ch.Target
// is empty. Admins only.
func (s *Service) SetStatusChannel(ctx context.Context, o Owner, ch events.Channel) error {
	if !o.Admin {
		return ErrNotAdmin
	}
	raw, err := json.Marshal(ch)
	if err != nil {
		return err
	}
	if err := s.q.SetSetting(ctx, subscriptionsdb.SetSettingParams{Key: statusChannelKey, Value: raw}); err != nil {
		return fmt.Errorf("status channel: %w", err)
	}
	return nil
}
