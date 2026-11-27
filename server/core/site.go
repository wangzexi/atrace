package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// The site and its owner: the title, and which person the owner is.

func (s *Store) SiteTitle(ctx context.Context, fallback string) (string, error) {
	var title sql.NullString
	err := s.reads.QueryRowContext(ctx, `SELECT title FROM site WHERE id=1`).Scan(&title)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !title.Valid) {
		return fallback, nil
	}
	return title.String, err
}

func (s *Store) SetSiteTitle(ctx context.Context, title string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO site (id,title) VALUES (1,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title`, title)
	return err
}

func (s *Store) SiteFooter(ctx context.Context) (string, error) {
	var footer string
	err := s.reads.QueryRowContext(ctx, `SELECT html FROM site_footer WHERE id=1`).Scan(&footer)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return footer, err
}

func (s *Store) SetSiteFooter(ctx context.Context, footer string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO site_footer (id,html) VALUES (1,?)
		ON CONFLICT(id) DO UPDATE SET html=excluded.html`, footer)
	return err
}

// SetOwner makes the person the one the owner is: the timeline they open on is what that person's accounts did, and
// their name is the one on those accounts. It is sql.ErrNoRows for a person there is not.
func (s *Store) SetOwner(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `INSERT INTO site (id,owner) SELECT 1, id FROM person WHERE id=? ON CONFLICT(id) DO UPDATE SET owner=excluded.owner`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// OfferedAccounts are the accounts the devices say their browsers are signed in to that belong to no person yet, with
// the names their platforms give them: what the owner may give to a person.
func (s *Store) OfferedAccounts(ctx context.Context, offered []string) ([]PersonAccount, error) {
	if offered == nil {
		offered = []string{}
	}
	list, err := json.Marshal(offered)
	if err != nil {
		return nil, err
	}
	rows, err := s.reads.QueryContext(ctx, `
 SELECT value, coalesce((SELECT json_extract(document,'$.name') FROM object_version WHERE object_id=value ORDER BY id DESC LIMIT 1),'')
 FROM json_each(?) WHERE value NOT IN (SELECT account FROM person_account) ORDER BY value`, string(list))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []PersonAccount{}
	for rows.Next() {
		var a PersonAccount
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// KnownAccount reports whether the store has met the account: as the actor of an activity, as the author of a post,
// or as one a person already has.
func (s *Store) KnownAccount(ctx context.Context, id string) (bool, error) {
	var known bool
	err := s.reads.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM activity WHERE actor=?1) OR EXISTS(SELECT 1 FROM object_version WHERE attributed_to=?1 OR object_id=?1)
		OR EXISTS(SELECT 1 FROM person_account WHERE account=?1)`, id).Scan(&known)
	return known, err
}

// OwnerNameForAccount is the name the owner goes by when the account is theirs, and empty for any other. Only
// the owner's name is given out: the names the owner gave other people are the owner's own notes.
func (s *Store) OwnerNameForAccount(ctx context.Context, id string) (string, error) {
	var name string
	err := s.reads.QueryRowContext(ctx, `
 SELECT coalesce((SELECT p.name FROM person_account a JOIN person p ON p.id=a.person_id
   WHERE a.account=? AND p.id=(SELECT owner FROM site WHERE id=1)), '')`, id).Scan(&name)
	return name, err
}
