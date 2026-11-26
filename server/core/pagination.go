package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// PageRange uses an exclusive activity-ref cursor and a half-open platform-date range.
type PageRange struct{ After, Since, Until, Visibility string }

var ErrCursor = errors.New("after must identify an available activity")
var cursorRef = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func ParsePageRange(after, since, until, visibility string) (PageRange, error) {
	f := PageRange{After: after, Visibility: visibility}
	if visibility != "" && visibility != "all" && visibility != "public" && visibility != "private" {
		return f, errors.New("visibility must be all, public or private")
	}
	if after != "" && !cursorRef.MatchString(after) {
		return f, ErrCursor
	}
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	for _, v := range []struct {
		name, value string
		dst         *string
	}{{"since", since, &f.Since}, {"until", until, &f.Until}} {
		if v.value == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, v.value)
		if err != nil {
			at, err = time.ParseInLocation("2006-01-02", v.value, zone)
		}
		if err != nil {
			return f, fmt.Errorf("%s must be YYYY-MM-DD or RFC3339", v.name)
		}
		*v.dst = at.UTC().Format(time.RFC3339Nano)
	}
	if f.Since != "" && f.Until != "" {
		a, _ := time.Parse(time.RFC3339Nano, f.Since)
		b, _ := time.Parse(time.RFC3339Nano, f.Until)
		if !a.Before(b) {
			return f, errors.New("since must be before until")
		}
	}
	return f, nil
}

const timelineDate = `coalesce(a.published_at, ` + objectPublished + `)`

func pageOrder(date string) string {
	return date + ` IS NULL, julianday(coalesce(` + date + `,o.observed_at)) DESC, a.id DESC`
}

// pageClause uses the same key as ORDER BY. Unknown platform dates follow all known dates.
func (s *Store) pageClause(ctx context.Context, f PageRange, publicOnly bool, date string) (string, []any, error) {
	clause := ""
	switch f.Visibility {
	case "public":
		clause += " AND EXISTS (SELECT 1 FROM activity_public vp WHERE vp.activity_id=a.id)"
	case "private":
		clause += " AND NOT EXISTS (SELECT 1 FROM activity_public vp WHERE vp.activity_id=a.id)"
	}
	var args []any
	if f.Since != "" {
		clause += " AND julianday(" + date + ")>=julianday(?)"
		args = append(args, f.Since)
	}
	if f.Until != "" {
		clause += " AND julianday(" + date + ")<julianday(?)"
		args = append(args, f.Until)
	}
	if f.After != "" {
		var id int64
		var unknown bool
		var at float64
		visible := ""
		if publicOnly {
			visible = " AND EXISTS (SELECT 1 FROM activity_public p WHERE p.activity_id=a.id)"
		}
		err := s.reads.QueryRowContext(ctx, `SELECT a.id,`+date+` IS NULL,julianday(coalesce(`+date+`,o.observed_at)) FROM activity a JOIN observation o ON o.activity_id=a.id WHERE a.ref=?`+visible, f.After).Scan(&id, &unknown, &at)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, ErrCursor
		}
		if err != nil {
			return "", nil, err
		}
		key := `julianday(coalesce(` + date + `,o.observed_at))`
		clause += ` AND ((` + date + ` IS NULL)>? OR ((` + date + ` IS NULL)=? AND (` + key + `<? OR (` + key + `=? AND a.id<?))))`
		args = append(args, unknown, unknown, at, at, id)
	}
	return clause, args, nil
}

// TimelinePage lists active entries by platform time (activity, then post), with unknown dates last.
// Within the same time, activity ID fixes the order. Person 0 selects the owner.
func (s *Store) TimelinePage(ctx context.Context, person int64, kinds []string, limit int, publicOnly bool, text string, regex bool, page PageRange) ([]Item, error) {
	return s.timeline(ctx, person, kinds, limit, "", publicOnly, text, regex, page)
}
