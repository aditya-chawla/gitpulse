// Package store holds plain-SQL persistence for users and sessions.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")

type User struct {
	ID              int64
	NodeID          string
	Login           string
	Name            string
	AvatarURL       string
	TokenCiphertext []byte
	TokenNonce      []byte
	TokenScopes     string
}

type Store struct{ DB *pgxpool.Pool }

func New(db *pgxpool.Pool) *Store { return &Store{DB: db} }

// UpsertUser inserts or refreshes the user and their encrypted token, and makes
// sure a settings row exists.
func (s *Store) UpsertUser(ctx context.Context, u User) error {
	return pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO users (id, node_id, login, name, avatar_url, token_ciphertext, token_nonce, token_scopes)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (id) DO UPDATE SET
			  node_id = EXCLUDED.node_id, login = EXCLUDED.login, name = EXCLUDED.name,
			  avatar_url = EXCLUDED.avatar_url, token_ciphertext = EXCLUDED.token_ciphertext,
			  token_nonce = EXCLUDED.token_nonce, token_scopes = EXCLUDED.token_scopes,
			  updated_at = now()`,
			u.ID, u.NodeID, u.Login, u.Name, u.AvatarURL, u.TokenCiphertext, u.TokenNonce, u.TokenScopes)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO user_settings (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, u.ID)
		return err
	})
}

const (
	userCols  = `id, node_id, login, coalesce(name,''), coalesce(avatar_url,''), token_ciphertext, token_nonce, token_scopes`
	userColsU = `u.id, u.node_id, u.login, coalesce(u.name,''), coalesce(u.avatar_url,''), u.token_ciphertext, u.token_nonce, u.token_scopes`
)

func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.NodeID, &u.Login, &u.Name, &u.AvatarURL, &u.TokenCiphertext, &u.TokenNonce, &u.TokenScopes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.DB.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

// ListUsers returns all users; the cron scan iterates over them.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+userCols+` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, userID int64, expires time.Time) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1,$2,$3)`,
		tokenHash, userID, expires)
	return err
}

// SessionUser resolves a live session to its user.
func (s *Store) SessionUser(ctx context.Context, tokenHash []byte) (*User, error) {
	return scanUser(s.DB.QueryRow(ctx, `
		SELECT `+userColsU+`
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, tokenHash))
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := s.DB.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	return tag.RowsAffected(), err
}
