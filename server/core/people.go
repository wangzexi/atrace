package core

import (
	"context"
	"database/sql"
	"errors"
	"unicode/utf8"
)

// People: the owner's notes on who is who. A person is a name and the accounts, on any platform, that are theirs.

// PersonAccount is an account that belongs to a person, with the name its platform gives it.
type PersonAccount struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Person is one person and their accounts. Owner is set on the person the owner is.
type Person struct {
	ID       int64           `json:"id"`
	Name     string          `json:"name"`
	Owner    bool            `json:"owner"`
	Accounts []PersonAccount `json:"accounts"`
}

// PersonNameForAccount resolves the configured display name for any person's account.
func (s *Store) PersonNameForAccount(ctx context.Context, id string) (string, error) {
	var name string
	err := s.reads.QueryRowContext(ctx, `SELECT coalesce((SELECT p.name FROM person_account a JOIN person p ON p.id=a.person_id WHERE a.account=?), '')`, id).Scan(&name)
	return name, err
}

// People lists everyone, the owner first, with their accounts.
func (s *Store) People(ctx context.Context) ([]Person, error) {
	rows, err := s.reads.QueryContext(ctx, `
 SELECT p.id, p.name, coalesce(p.id=(SELECT owner FROM site WHERE id=1), 0), coalesce(a.account,''),
   coalesce((SELECT json_extract(v.document,'$.name') FROM object_version v WHERE v.object_id=a.account ORDER BY v.id DESC LIMIT 1),'')
 FROM person p LEFT JOIN person_account a ON a.person_id=p.id
 ORDER BY p.id=(SELECT owner FROM site WHERE id=1) DESC, coalesce((SELECT position FROM person_position WHERE person_id=p.id), 9223372036854775807), p.name, p.id, a.account`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	people := []Person{}
	for rows.Next() {
		var p Person
		var a PersonAccount
		if err := rows.Scan(&p.ID, &p.Name, &p.Owner, &a.ID, &a.Name); err != nil {
			return nil, err
		}
		if n := len(people); n == 0 || people[n-1].ID != p.ID {
			p.Accounts = []PersonAccount{}
			people = append(people, p)
		}
		if a.ID != "" {
			last := &people[len(people)-1]
			last.Accounts = append(last.Accounts, a)
		}
	}
	return people, rows.Err()
}

// CheckPerson rejects a name that is empty or too long and an account the store has not met: what a person, the
// owner included, may be saved with.
func (s *Store) CheckPerson(ctx context.Context, name string, accounts []string) error {
	if name == "" || utf8.RuneCountInString(name) > 80 {
		return badf("a person needs a name of at most 80 characters")
	}
	for _, id := range accounts {
		met, err := s.KnownAccount(ctx, id)
		if err != nil {
			return err
		}
		if !met {
			return badf("%s is not an account the archive has met", id)
		}
	}
	return nil
}

// CreatePerson makes a person with the accounts. An account that belonged to another person moves to this one.
func (s *Store) CreatePerson(ctx context.Context, name string, accounts []string) (int64, error) {
	if err := s.CheckPerson(ctx, name, accounts); err != nil {
		return 0, err
	}
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO person (name) VALUES (?)`, name)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		return setAccounts(ctx, tx, id, accounts)
	})
	return id, err
}

// UpdatePerson renames a person and gives them exactly the accounts. It is sql.ErrNoRows for a person there is not.
func (s *Store) UpdatePerson(ctx context.Context, id int64, name string, accounts []string) error {
	if err := s.CheckPerson(ctx, name, accounts); err != nil {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE person SET name=? WHERE id=?`, name, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return sql.ErrNoRows
		}
		return setAccounts(ctx, tx, id, accounts)
	})
}

// DeletePerson removes a person; their accounts go back to being known only by their own names. The owner cannot be removed.
func (s *Store) DeletePerson(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var owner bool
		err := tx.QueryRowContext(ctx, `SELECT coalesce(id=(SELECT owner FROM site WHERE id=1), 0) FROM person WHERE id=?`, id).Scan(&owner)
		if err != nil {
			return err
		}
		if owner {
			return badf("the owner cannot be deleted")
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM person WHERE id=?`, id)
		return err
	})
}

// PersonOfAccount is the person the account belongs to, or 0 when it belongs to none.
func (s *Store) PersonOfAccount(ctx context.Context, account string) (int64, error) {
	var id int64
	err := s.reads.QueryRowContext(ctx, `SELECT person_id FROM person_account WHERE account=?`, account).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// setAccounts gives the person exactly these accounts, taking them from whoever had them.
func setAccounts(ctx context.Context, tx *sql.Tx, person int64, accounts []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM person_account WHERE person_id=?`, person); err != nil {
		return err
	}
	for _, account := range accounts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO person_account (account,person_id) VALUES (?,?) ON CONFLICT(account) DO UPDATE SET person_id=excluded.person_id`, account, person); err != nil {
			return err
		}
	}
	return nil
}

// OrderPeople saves the complete list atomically; its first person is the default timeline.
func (s *Store) OrderPeople(ctx context.Context, ids []int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM person").Scan(&count); err != nil {
			return err
		}
		if len(ids) == 0 || len(ids) != count {
			return badf("order must contain every person exactly once")
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM person_position"); err != nil {
			return err
		}
		seen := map[int64]bool{}
		for position, id := range ids {
			if seen[id] {
				return badf("order must contain every person exactly once")
			}
			seen[id] = true
			result, err := tx.ExecContext(ctx, "INSERT INTO person_position (person_id, position) SELECT id, ? FROM person WHERE id=?", position, id)
			if err != nil {
				return err
			}
			if n, _ := result.RowsAffected(); n != 1 {
				return badf("unknown person in order")
			}
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO site (id,owner) VALUES (1,?) ON CONFLICT(id) DO UPDATE SET owner=excluded.owner", ids[0])
		return err
	})
}
