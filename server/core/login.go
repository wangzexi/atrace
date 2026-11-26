package core

import (
	"context"
	"database/sql"
	"errors"
)

// The owner's login: the salt and verifier of the password, and the key the login cookies are signed with. A
// password is only ever kept as a verifier derived from it; the key changes with the password, so the cookies a
// change of password leaves behind stop working.

// Login is the owner's login as the store keeps it.
type Login struct {
	Salt, Verifier, Key []byte
}

// OwnerLogin is the owner's login, or sql.ErrNoRows while no password has been set.
func (s *Store) OwnerLogin(ctx context.Context) (Login, error) {
	var l Login
	err := s.reads.QueryRowContext(ctx, `SELECT salt, verifier, session_key FROM owner_login WHERE id=1`).Scan(&l.Salt, &l.Verifier, &l.Key)
	if errors.Is(err, sql.ErrNoRows) {
		return Login{}, sql.ErrNoRows
	}
	return l, err
}

// SetOwnerLogin replaces the owner's login.
func (s *Store) SetOwnerLogin(ctx context.Context, l Login) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO owner_login (id,salt,verifier,session_key) VALUES (1,?,?,?)
		ON CONFLICT(id) DO UPDATE SET salt=excluded.salt, verifier=excluded.verifier, session_key=excluded.session_key`,
		l.Salt, l.Verifier, l.Key)
	return err
}
