package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// schemaVersion is stored in SQLite PRAGMA user_version.
const schemaVersion = 11

// upgrade creates the schema in a new database, or migrates the one written by the previous version.
func upgrade(db *sql.DB) error {
	var tables int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='activity'`).Scan(&tables); err != nil {
		return err
	}
	if tables == 0 {
		if _, err := db.Exec(schemaSQL); err != nil {
			return err
		}
		_, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion))
		return err
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	switch version {
	case schemaVersion:
		// Additive tables can be opened by older builds during a rollback.
		_, err := db.Exec(schemaSQL)
		return err
	case 7:
		if err := migrate(db); err != nil {
			return err
		}
		if err := renamePlatforms(db); err != nil {
			return err
		}
		return addTables(db)
	case 8:
		if err := renamePlatforms(db); err != nil {
			return err
		}
		return addTables(db)
	case 9, 10:
		return addTables(db)
	}
	return fmt.Errorf("the database is at schema version %d; this atrace reads %d and migrates from 7, 8, 9 or 10", version, schemaVersion)
}

// addTables creates the tables a version added since the database was written. The schema only uses IF NOT EXISTS,
// so it can run over a database that already has most of it.
func addTables(db *sql.DB) error {
	if _, err := db.Exec(schemaSQL); err != nil {
		return err
	}
	_, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion))
	return err
}

// migrate is version 7 to 8: the object table is gone (an object's type is its document's), the activity
// carries its own ref, the tables of the collectors' runs, queue, schedules and accounts are gone (a device keeps
// those), the owner is a person with the accounts that were bound, every post with an author has a Create, and the
// media table loses what nothing read.
//
// object_version is rebuilt, which SQLite wants done with foreign keys off (the observation table points at
// it), so the step runs on the one connection with them off and checks them before it turns them on.
func migrate(db *sql.DB) error {
	// legacy_alter_table keeps a rename from rewriting the tables that point at the renamed one.
	for _, pragma := range []string{`PRAGMA foreign_keys = OFF`, `PRAGMA legacy_alter_table = ON`} {
		if _, err := db.Exec(pragma); err != nil {
			return err
		}
	}
	defer db.Exec(`PRAGMA legacy_alter_table = OFF`)
	defer db.Exec(`PRAGMA foreign_keys = ON`)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	step := func(statements ...string) error {
		for _, statement := range statements {
			if _, err := tx.Exec(statement); err != nil {
				return fmt.Errorf("migration: %w", err)
			}
		}
		return nil
	}
	err = step(
		// What changes in a table that stays.
		`ALTER TABLE object_version RENAME TO object_version_old`,
		`DROP INDEX object_version_object`,
		`DROP INDEX object_version_author`,
		`DROP INDEX object_version_reply`,
		`DROP INDEX object_version_quote`,
		`ALTER TABLE activity ADD COLUMN ref TEXT`,
		`UPDATE activity SET ref=(SELECT ref FROM activity_ref WHERE activity_id=activity.id)`,
		`DROP TABLE activity_ref`,
		`ALTER TABLE media DROP COLUMN size`,
		`ALTER TABLE media DROP COLUMN stored_at`,
	)
	// The new tables and the indexes come from the schema file, so a migrated database is a new one's twin.
	if err == nil {
		_, err = tx.Exec(schemaSQL)
	}
	if err == nil {
		err = step(
			`INSERT INTO object_version (id,object_id,document,content_hash,attributed_to,in_reply_to,quote_of,published_at,updated_at)
				SELECT id,object_id,document,content_hash,attributed_to,in_reply_to,quote_of,published_at,updated_at FROM object_version_old`,
			`DROP TABLE object_version_old`,
			`DROP TABLE object`,
			// What the server kept of the collectors (their runs, queue, schedules and accounts) is the collectors' own now.
			`DROP TABLE IF EXISTS collector_run`,
			`DROP TABLE IF EXISTS collector_task`,
			`DROP TABLE IF EXISTS collector_state`,
			`DROP TABLE IF EXISTS collector_interval`,
			`DROP TABLE IF EXISTS collector_cron`,
			`DROP TABLE IF EXISTS collector_probe`,
			`DROP TABLE IF EXISTS collector_account_seen`,
			// the site, and its owner as a person with the accounts the owner had bound
			`INSERT INTO site (id,title) SELECT 1,(SELECT title FROM site_settings WHERE id=1)
				WHERE EXISTS (SELECT 1 FROM site_settings) OR EXISTS (SELECT 1 FROM owner_profile)`,
			`INSERT INTO person (id,name) SELECT 1,coalesce(display_name,'') FROM owner_profile WHERE id=1`,
			`UPDATE site SET owner=1 WHERE EXISTS (SELECT 1 FROM person WHERE id=1)`,
			`INSERT OR IGNORE INTO person_account (account,person_id) SELECT value,1 FROM owner_profile, json_each(coalesce(owner_profile.accounts,'[]')) WHERE owner_profile.id=1`,
			`DROP TABLE site_settings`,
			`DROP TABLE owner_profile`,
			// the entry search is derived from what is stored, and is built again when the store opens
			`DROP TABLE IF EXISTS entry_search`,
			`PRAGMA user_version = 8`,
		)
	}
	if err == nil {
		err = backfillCreates(tx)
	}
	if err == nil {
		var broken int
		if err = tx.QueryRow(`SELECT count(*) FROM pragma_foreign_key_check`).Scan(&broken); err == nil && broken > 0 {
			err = fmt.Errorf("migration: %d rows break a foreign key", broken)
		}
	}
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// migrationObserver is what the observation of an activity the migration made says observed it: no collector did.
const migrationObserver = "migration"

// backfillCreates gives every post that has an author and no Create the Create it would have had if a collector
// had written it for someone else's post as it does for the owner's: actor is the author, published is the
// post's. The observation is the migration's, with the time the post was first observed. Collections, people and
// the like are not posts and get none.
func backfillCreates(tx *sql.Tx) error {
	ctx := context.Background()
	now := formatTime(time.Now())
	rows, err := tx.QueryContext(ctx, `
 SELECT v.object_id, v.attributed_to, coalesce(v.published_at,''),
   coalesce((SELECT min(o.observed_at) FROM object_version w JOIN observation o ON o.version_id=w.id WHERE w.object_id=v.object_id), ?)
 FROM object_version v
 WHERE v.id=(SELECT max(id) FROM object_version WHERE object_id=v.object_id)
   AND coalesce(v.attributed_to,'')<>''
   AND json_extract(v.document,'$.type') IN ('Note','Article','Video')
   AND NOT EXISTS (SELECT 1 FROM activity a WHERE a.kind='Create' AND a.object=v.object_id)
   AND NOT EXISTS (SELECT 1 FROM activity k WHERE k.object=v.object_id AND k.kind IN ('Add','Like') AND k.actor=v.attributed_to)
 ORDER BY v.id`, now)
	if err != nil {
		return fmt.Errorf("migration: %w", err)
	}
	type post struct{ id, author, published, observed string }
	var posts []post
	for rows.Next() {
		var p post
		if err := rows.Scan(&p.id, &p.author, &p.published, &p.observed); err != nil {
			rows.Close()
			return err
		}
		posts = append(posts, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, p := range posts {
		doc := map[string]any{"type": "Create", "actor": p.author, "object": p.id}
		if p.published != "" {
			doc["published"] = p.published
		}
		body, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		ref, err := newRef(ctx, tx)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO activity (type,kind,actor,object,published_at,document,ref) VALUES ('Create','Create',?,?,nullif(?,''),?,?)`,
			p.author, p.id, p.published, string(body), ref)
		if err != nil {
			return fmt.Errorf("migration: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO observation (activity_id,observed_at,observed_by) VALUES (?,?,?)`, id, p.observed, migrationObserver); err != nil {
			return fmt.Errorf("migration: %w", err)
		}
	}
	return nil
}
