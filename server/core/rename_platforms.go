package core

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
)

func renamedPlatformID(id string) string {
	switch platform, rest, _ := strings.Cut(id, ":"); platform {
	case "blog":
		return "github:" + rest
	case "xhs":
		return "xiaohongshu:" + rest
	default:
		return id
	}
}

func renamePlatforms(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, pair := range [][2]string{{"blog:", "github:"}, {"xhs:", "xiaohongshu:"}} {
		var collision bool
		err := tx.QueryRow(`SELECT EXISTS (
			SELECT 1 FROM object_version AS old
			JOIN object_version AS current ON current.object_id = ? || substr(old.object_id, ?)
			WHERE old.object_id GLOB ?
		)`, pair[1], len(pair[0])+1, pair[0]+"*").Scan(&collision)
		if err != nil {
			return err
		}
		if collision {
			return fmt.Errorf("cannot rename %s IDs: matching %s IDs already exist", pair[0], pair[1])
		}
		for _, column := range [][2]string{{"person_account", "account"}, {"object_gone", "object_id"}} {
			query := fmt.Sprintf("UPDATE %s SET %s = ? || substr(%s, ?) WHERE %s GLOB ?", column[0], column[1], column[1], column[1])
			if _, err := tx.Exec(query, pair[1], len(pair[0])+1, pair[0]+"*"); err != nil {
				return err
			}
		}
	}
	for _, table := range []string{"object_version", "activity"} {
		if err := renameDocumentIDs(tx, table); err != nil {
			return fmt.Errorf("rename IDs in %s: %w", table, err)
		}
	}
	if _, err := tx.Exec(`DROP TABLE IF EXISTS entry_search`); err != nil {
		return err
	}
	if _, err := tx.Exec(`PRAGMA user_version = 9`); err != nil {
		return err
	}
	return tx.Commit()
}

func renameDocumentIDs(tx *sql.Tx, table string) error {
	var after int64
	for {
		rows, err := tx.Query("SELECT id, document FROM "+table+
			" WHERE id > ? AND (instr(document, 'blog:') > 0 OR instr(document, 'xhs:') > 0) ORDER BY id LIMIT 256", after)
		if err != nil {
			return err
		}
		type row struct {
			id       int64
			document []byte
		}
		var batch []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.document); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, r := range batch {
			after = r.id
			doc, err := decodeDocument(r.document)
			if err != nil {
				return err
			}
			changed := false
			for _, key := range []string{"id", "actor", "object", "target", "attributedTo", "inReplyTo", "quote"} {
				if old, ok := doc[key].(string); ok {
					if next := renamedPlatformID(old); next != old {
						doc[key] = next
						changed = true
					}
				}
			}
			if !changed {
				continue
			}
			canonical, err := canonicalJSON(doc)
			if err != nil {
				return err
			}
			if table == "object_version" {
				o, err := parseObject(doc)
				if err != nil {
					return err
				}
				hash := fmt.Sprintf("%x", sha256.Sum256(canonical))
				_, err = tx.Exec(`UPDATE object_version SET document=?, content_hash=?, object_id=?, attributed_to=NULLIF(?,''), in_reply_to=NULLIF(?,''), quote_of=NULLIF(?,'') WHERE id=?`,
					string(canonical), hash, o.id, o.attributedTo, o.inReplyTo, o.quote, r.id)
				if err != nil {
					return err
				}
			} else {
				typ, _ := doc["type"].(string)
				a, err := parseActivity(typ, doc)
				if err != nil {
					return err
				}
				_, err = tx.Exec(`UPDATE activity SET document=?, actor=?, object=?, target=NULLIF(?,''), announce_id=NULLIF(?,'') WHERE id=?`,
					string(canonical), a.actor, a.object, a.target, a.announceID, r.id)
				if err != nil {
					return err
				}
			}
		}
	}
}
