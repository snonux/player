package repository

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Timestamps are stored as text, and several queries compare or sort them as
// text (share and session expiry, podcast scheduling, every ORDER BY on a
// time column). Text order equals time order only if all values are written
// in one zone and one layout.
//
// The driver writes a time.Time with time.String(), in whatever zone the
// value happens to carry: "2026-10-09 13:26:35 +0000 UTC" for a time parsed
// from JSON, "2026-10-09 16:16:35 +0300 EEST" for time.Now() on a server in
// that zone. Compared as text those are 3 hours apart in the wrong
// direction, so on a non-UTC server a share with an explicit expires_at
// stopped (or failed to stop) at the wrong moment.
//
// Two measures make text order correct:
//
//   - utcTimesDSN makes the driver convert every time it writes or binds to
//     UTC (and every time it reads), so new values and query arguments share
//     one zone and layout.
//   - normalizeStoredTimes rewrites rows that older versions stored in
//     another zone, once, at startup.
//
// Values written by SQLite itself (DEFAULT CURRENT_TIMESTAMP,
// "2026-10-09 13:26:35") are UTC already and sort correctly against the
// driver's layout to the second, which is all the callers need.

// utcTimeSuffix ends every timestamp the driver writes in UTC.
const utcTimeSuffix = " +0000 UTC"

// goTimeLayout is time.String() without the monotonic clock reading.
const goTimeLayout = "2006-01-02 15:04:05.999999999 -0700 MST"

// timeColumns lists every timestamp column, by table. A column added to the
// schema belongs here too, or rows a non-UTC writer left in it stay as they
// are.
var timeColumns = map[string][]string{
	"users":                {"created_at"},
	"api_tokens":           {"last_used_at", "expires_at", "created_at"},
	"sets":                 {"created_at"},
	"set_permissions":      {"created_at"},
	"media":                {"deleted_at", "created_at"},
	"favorites":            {"created_at"},
	"playback_progress":    {"updated_at"},
	"sessions":             {"created_at", "expires_at"},
	"playback_accumulator": {"updated_at"},
	"shares":               {"created_at", "expires_at"},
	"media_notes":          {"created_at", "updated_at"},
	"podcast_feeds":        {"last_checked_at", "next_check_at", "created_at"},
	"podcast_episodes":     {"published_at", "created_at"},
	"podcast_status":       {"updated_at"},
}

// utcTimesDSN returns dsn with the driver option that makes it write, bind
// and read every time.Time in UTC. A DSN that already chooses a time zone is
// left alone.
func utcTimesDSN(dsn string) string {
	if strings.Contains(dsn, "_timezone=") {
		return dsn
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + "_timezone=UTC"
}

// normalizeStoredTimes rewrites every stored timestamp that carries a zone
// other than UTC into the UTC form the driver writes now. It is idempotent
// and, once done, only costs one scan per column at startup.
func normalizeStoredTimes(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for table, columns := range timeColumns {
		for _, column := range columns {
			if err := normalizeTimeColumn(tx, table, column); err != nil {
				return fmt.Errorf("%s.%s: %w", table, column, err)
			}
		}
	}
	return tx.Commit()
}

// storedTime is one timestamp found in a foreign zone.
type storedTime struct {
	rowID int64
	utc   time.Time
}

// normalizeTimeColumn converts the non-UTC values of one column. The names
// come from timeColumns, never from input, so building the SQL from them is
// safe.
func normalizeTimeColumn(tx *sql.Tx, table, column string) error {
	found, err := foreignZoneTimes(tx, table, column)
	if err != nil {
		return err
	}
	for _, st := range found {
		// Bound as text in the driver's own layout, so the result does not
		// depend on how the connection was opened.
		text := st.utc.Format(goTimeLayout)
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s SET %s = ? WHERE rowid = ?`, table, column), text, st.rowID); err != nil {
			return fmt.Errorf("update: %w", err)
		}
	}
	return nil
}

// foreignZoneTimes returns the values of a column that are written in Go's
// time.String() layout with a zone other than UTC. Values in any other shape
// (SQLite's own CURRENT_TIMESTAMP, anything unparseable) are left alone.
func foreignZoneTimes(tx *sql.Tx, table, column string) ([]storedTime, error) {
	// CAST keeps the driver from parsing the value: the raw text is needed.
	// The LIKE patterns match " +hhmm " / " -hhmm " as time.String() writes
	// the offset, and skip what is in UTC already.
	rows, err := tx.Query(fmt.Sprintf(
		`SELECT rowid, CAST(%[2]s AS TEXT) FROM %[1]s
		 WHERE (%[2]s LIKE '%% +____ %%' OR %[2]s LIKE '%% -____ %%') AND %[2]s NOT LIKE ?`,
		table, column), "%"+utcTimeSuffix+"%")
	if err != nil {
		return nil, fmt.Errorf("select: %w", err)
	}
	defer rows.Close()
	var found []storedTime
	for rows.Next() {
		var rowID int64
		var text string
		if err := rows.Scan(&rowID, &text); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if t, ok := parseGoTime(text); ok {
			found = append(found, storedTime{rowID: rowID, utc: t.UTC()})
		}
	}
	return found, rows.Err()
}

// parseGoTime parses time.String() output, with or without the monotonic
// clock reading (" m=+12.3") that time.Now() values carry.
func parseGoTime(text string) (time.Time, bool) {
	if i := strings.Index(text, " m="); i >= 0 {
		text = text[:i]
	}
	t, err := time.Parse(goTimeLayout, strings.TrimSpace(text))
	return t, err == nil
}
