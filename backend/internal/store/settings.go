package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"quizroom/internal/rating"
)

// DefaultSettings apply until the learner saves their own.
var DefaultSettings = Settings{Target: rating.DefaultTarget, Model: "sonnet"}

// Settings returns the saved settings, or the defaults.
func (s *Store) Settings(ctx context.Context) (Settings, error) {
	var v Settings
	err := s.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = 'settings'`).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultSettings, nil
	}
	return v, err
}

// SaveSettings replaces the settings.
func (s *Store) SaveSettings(ctx context.Context, v Settings) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO settings (key, value) VALUES ('settings', $1)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, v)
	return err
}
