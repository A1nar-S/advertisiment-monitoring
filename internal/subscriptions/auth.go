package subscriptions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/A1nar-S/advertisiment-monitoring/internal/subscriptions/subscriptionsdb"
)

// Dashboard login: a bot sends a one-time link token; opening the link trades
// it for a session. Both are random 32-byte tokens; only their SHA-256 is
// stored, so a database leak gives nobody a way in.

const (
	LoginTokenTTL = 10 * time.Minute
	SessionTTL    = 30 * 24 * time.Hour
)

// ErrBadToken: the token is unknown, expired or already used.
var ErrBadToken = errors.New("invalid or expired token")

// NewLoginToken issues a one-time login token for the owner.
func (s *Service) NewLoginToken(ctx context.Context, o Owner) (string, error) {
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	err = s.q.InsertLoginToken(ctx, subscriptionsdb.InsertLoginTokenParams{
		TokenHash: hash, UserID: o.UserID, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(LoginTokenTTL), Valid: true},
	})
	return token, err
}

// Login trades a login token (single use) for a session token.
func (s *Service) Login(ctx context.Context, loginToken string) (string, Owner, error) {
	userID, err := s.q.ConsumeLoginToken(ctx, hashToken(loginToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", Owner{}, ErrBadToken
	}
	if err != nil {
		return "", Owner{}, err
	}
	session, hash, err := newToken()
	if err != nil {
		return "", Owner{}, err
	}
	err = s.q.InsertSession(ctx, subscriptionsdb.InsertSessionParams{
		TokenHash: hash, UserID: userID, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(SessionTTL), Valid: true},
	})
	if err != nil {
		return "", Owner{}, err
	}
	u, err := s.q.GetUser(ctx, userID)
	if err != nil {
		return "", Owner{}, err
	}
	return session, s.owner(u.ID, u.Platform, u.ExternalID, u.DisplayName), nil
}

// SessionOwner returns who a session token belongs to.
func (s *Service) SessionOwner(ctx context.Context, session string) (Owner, error) {
	u, err := s.q.GetSessionUser(ctx, hashToken(session))
	if errors.Is(err, pgx.ErrNoRows) {
		return Owner{}, ErrBadToken
	}
	if err != nil {
		return Owner{}, err
	}
	return s.owner(u.ID, u.Platform, u.ExternalID, u.DisplayName), nil
}

// Logout ends a session.
func (s *Service) Logout(ctx context.Context, session string) error {
	return s.q.DeleteSession(ctx, hashToken(session))
}

// PurgeExpiredAuth deletes expired login tokens and sessions.
func (s *Service) PurgeExpiredAuth(ctx context.Context) error { return s.q.PurgeExpiredAuth(ctx) }

func newToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
