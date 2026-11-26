package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Stored files and the media they copy.

// PutFile stores a content-addressed file. The returned ref depends only on
// bytes, so the same file can be shared by any document. When sourceURL is
// given, the file is registered as the local copy of that platform URL.
func (s *Store) PutFile(ctx context.Context, body io.Reader, sourceURL, mime string) (string, error) {
	if sourceURL != "" {
		if err := validateURL("source_url", sourceURL); err != nil {
			return "", err
		}
	}
	if len(mime) > maxString {
		return "", badf("mime is too long")
	}
	tmp, err := os.CreateTemp(s.files, ".upload-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	hash := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmp, hash), body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	ref := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	path := s.path(ref)
	if err := os.Rename(tmp.Name(), path); err != nil {
		if _, statErr := os.Stat(path); statErr != nil {
			return "", err
		}
	}
	if sourceURL != "" {
		_, err = s.db.ExecContext(ctx, `INSERT INTO media (url,file_ref,mime) VALUES (?,?,?)
			ON CONFLICT(url) DO UPDATE SET file_ref=excluded.file_ref,mime=excluded.mime`,
			sourceURL, ref, mime)
		if err != nil {
			return "", err
		}
	}
	return ref, nil
}

// OpenFile returns the content of a stored file.
func (s *Store) OpenFile(ref string) (io.ReadCloser, error) {
	if !validFileRef(ref) {
		return nil, os.ErrNotExist
	}
	return os.Open(s.path(ref))
}

// PublicMedia reports whether a current public entry displays this file.
func (s *Store) PublicMedia(ctx context.Context, ref string) (bool, error) {
	if !validFileRef(ref) {
		return false, nil
	}
	var public bool
	err := s.reads.QueryRowContext(ctx, `WITH published AS (
		SELECT a.object, a.kind, v.quote_of, v.in_reply_to, v.attributed_to
		FROM activity_public p JOIN activity a ON a.id=p.activity_id
		JOIN object_version v ON v.id=(SELECT max(l.id) FROM object_version l WHERE l.object_id=a.object)
		WHERE 1=1`+activeClause+hiddenClause+`
	), displayed(id) AS (
		SELECT object FROM published
		UNION SELECT in_reply_to FROM published WHERE kind='Create' AND in_reply_to IS NOT NULL
		UNION SELECT quote_of FROM published WHERE quote_of IS NOT NULL AND (
			kind='Create' OR (kind IN ('Like','Add') AND attributed_to IN (
				SELECT account FROM person_account WHERE person_id=(SELECT owner FROM site WHERE id=1))))
	)
	SELECT EXISTS (
		SELECT 1 FROM displayed d
		JOIN object_version v ON v.id=(SELECT max(l.id) FROM object_version l WHERE l.object_id=d.id)
		JOIN json_each(v.document, '$.attachment') att
		JOIN media m ON m.url IN (json_extract(att.value, '$.url'),
			CASE WHEN json_extract(att.value, '$.type')='Video' AND json_extract(att.value, '$.preview.type')='Image'
			THEN json_extract(att.value, '$.preview.url') END)
		WHERE m.file_ref=?
	)`, ref).Scan(&public)
	return public, err
}

// MediaCopy is the local copy of a platform media URL: the file and the type it was uploaded as.
// ok is false when there is none.
func (s *Store) MediaCopy(ctx context.Context, url string) (ref, mime string, ok bool, err error) {
	err = s.reads.QueryRowContext(ctx, `SELECT file_ref, mime FROM media WHERE url=?`, url).Scan(&ref, &mime)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	return ref, mime, err == nil, err
}

// MediaRefs maps platform media URLs to the refs of their local copies. URLs
// without a local copy are absent from the result.
func (s *Store) MediaRefs(ctx context.Context, urls []string) (map[string]string, error) {
	refs := map[string]string{}
	for _, u := range urls {
		var ref string
		err := s.reads.QueryRowContext(ctx, `SELECT file_ref FROM media WHERE url=?`, u).Scan(&ref)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		refs[u] = ref
	}
	return refs, nil
}

func (s *Store) path(ref string) string {
	return filepath.Join(s.files, strings.TrimPrefix(ref, "sha256:"))
}

func validFileRef(ref string) bool {
	if !strings.HasPrefix(ref, "sha256:") || len(ref) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(ref, "sha256:"))
	return err == nil
}

// ValidFileRef checks a content-addressed file reference.
func ValidFileRef(ref string) bool { return validFileRef(ref) }
