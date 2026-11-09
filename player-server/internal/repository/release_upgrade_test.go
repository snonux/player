package repository

import (
	"compress/gzip"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// releaseDBs are empty-but-bootstrapped databases written by released
// servers (one admin user each), committed under testdata/releases.
var releaseDBs = []string{"v0.5.0", "v0.10.0", "v0.13.0", "v0.14.0", "v0.17.0"}

// restoreReleaseDB unpacks testdata/releases/<version>.db.gz into a
// temporary file and returns its path.
func restoreReleaseDB(t *testing.T, version string) string {
	t.Helper()
	in, err := os.Open(filepath.Join("testdata", "releases", version+".db.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	zr, err := gzip.NewReader(in)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "player.db")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, zr); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// schemaShape describes a database's tables, columns (order-insensitive:
// ALTER TABLE appends), indexes and foreign keys in comparable text.
func schemaShape(t *testing.T, db *sql.DB) string {
	t.Helper()
	tables := queryColumn(t, db, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	var lines []string
	for _, table := range tables {
		var cols []string
		rows, err := db.Query(`SELECT name, upper(type), "notnull", COALESCE(dflt_value, ''), pk FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var name, typ, dflt string
			var notNull, pk int
			if err := rows.Scan(&name, &typ, &notNull, &dflt, &pk); err != nil {
				t.Fatal(err)
			}
			cols = append(cols, fmt.Sprintf("  col %s %s notnull=%d default=%s pk=%d", name, typ, notNull, dflt, pk))
		}
		_ = rows.Close()
		sort.Strings(cols)
		lines = append(lines, "table "+table)
		lines = append(lines, cols...)
		var fks []string
		rows, err = db.Query(`SELECT "table", "from", COALESCE("to", ''), on_delete FROM pragma_foreign_key_list(?)`, table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var to, from, toCol, onDelete string
			if err := rows.Scan(&to, &from, &toCol, &onDelete); err != nil {
				t.Fatal(err)
			}
			fks = append(fks, fmt.Sprintf("  fk %s -> %s(%s) on delete %s", from, to, toCol, onDelete))
		}
		_ = rows.Close()
		sort.Strings(fks)
		lines = append(lines, fks...)
	}
	for _, idx := range queryColumn(t, db, `SELECT name || ' ON ' || tbl_name || ': ' || COALESCE(sql, 'auto') FROM sqlite_schema WHERE type = 'index' AND name NOT LIKE 'sqlite_autoindex_%' ORDER BY name`) {
		lines = append(lines, "index "+strings.Join(strings.Fields(idx), " "))
	}
	return strings.Join(lines, "\n")
}

func queryColumn(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// diffLines lists the lines only one side has.
func diffLines(want, got string) string {
	w, g := map[string]bool{}, map[string]bool{}
	for _, l := range strings.Split(want, "\n") {
		w[l] = true
	}
	for _, l := range strings.Split(got, "\n") {
		g[l] = true
	}
	var out []string
	for l := range w {
		if !g[l] {
			out = append(out, "- "+l)
		}
	}
	for l := range g {
		if !w[l] {
			out = append(out, "+ "+l)
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// allowKnownDivergence hides the one accepted difference: the
// music-libraries migration rebuilds sets with library_id NOT NULL (every
// migrated set is assigned the default library), while a fresh schema keeps
// the column nullable. Both reject nothing the server writes.
func allowKnownDivergence(shape string) string {
	const migrated = "table sets\n  col cover_thumbnail_path TEXT notnull=0 default= pk=0\n  col created_at DATETIME notnull=0 default=CURRENT_TIMESTAMP pk=0\n  col id INTEGER notnull=0 default= pk=1\n  col is_podcast INTEGER notnull=1 default=0 pk=0\n  col library_id INTEGER notnull=1"
	return strings.Replace(shape, migrated, strings.TrimSuffix(migrated, "1")+"0", 1)
}

// TestReleaseDatabasesUpgradeToFreshSchema opens databases written by past
// releases and checks that the migrated schema equals a fresh database's,
// that the release's data survived, and that a second start changes nothing.
func TestReleaseDatabasesUpgradeToFreshSchema(t *testing.T) {
	fresh, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	want := schemaShape(t, fresh.db)
	_ = fresh.Close()
	for _, version := range releaseDBs {
		t.Run(version, func(t *testing.T) {
			path := restoreReleaseDB(t, version)
			s, err := Open(path)
			if err != nil {
				t.Fatalf("open %s database: %v", version, err)
			}
			got := schemaShape(t, s.db)
			if norm := allowKnownDivergence(got); norm != want {
				t.Errorf("migrated %s schema differs from a fresh one:\n%s", version, diffLines(want, norm))
			}
			var users int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = 'admin'`).Scan(&users); err != nil || users != 1 {
				t.Errorf("release data lost: %d admin users, %v", users, err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			again, err := Open(path)
			if err != nil {
				t.Fatalf("second start: %v", err)
			}
			defer func() { _ = again.Close() }()
			if shape := schemaShape(t, again.db); shape != got {
				t.Errorf("second start changed the schema:\n%s", diffLines(got, shape))
			}
		})
	}
}

// TestSchemaStepsRunOnceAndRecord checks that a migration runs on the first
// start only, every-start steps run each time, a failed step is not recorded,
// and a duplicate step name is refused.
func TestSchemaStepsRunOnceAndRecord(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "steps.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	runs := map[string]int{}
	fail := true
	steps := []schemaStep{
		{name: "always", run: func(*sql.DB) error { runs["always"]++; return nil }, everyStart: true},
		{name: "once", run: func(*sql.DB) error { runs["once"]++; return nil }},
		{name: "flaky", run: func(*sql.DB) error {
			runs["flaky"]++
			if fail {
				return fmt.Errorf("boom")
			}
			return nil
		}},
	}
	if err := runSchemaSteps(db, steps); err == nil {
		t.Fatal("failed step reported success")
	}
	fail = false
	for i := 0; i < 2; i++ {
		if err := runSchemaSteps(db, steps); err != nil {
			t.Fatal(err)
		}
	}
	if runs["always"] != 3 || runs["once"] != 1 || runs["flaky"] != 2 {
		t.Fatalf("runs = %v", runs)
	}
	recorded := queryColumn(t, db, `SELECT name FROM schema_migrations ORDER BY name`)
	if strings.Join(recorded, ",") != "flaky,once" {
		t.Fatalf("recorded = %v", recorded)
	}
	if err := runSchemaSteps(db, append(steps, steps[1])); err == nil {
		t.Fatal("duplicate step name accepted")
	}
}

// TestSchemaStepNamesAreUnique guards the real step list.
func TestSchemaStepNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, step := range schemaSteps {
		if step.name == "" || seen[step.name] {
			t.Fatalf("bad or duplicate step name %q", step.name)
		}
		seen[step.name] = true
	}
}

// forgetSchemaMigrations makes a hand-downgraded test database look like
// one written before schema_migrations existed, so the next start re-runs
// every (idempotent) migration as a real pre-versioning upgrade does.
func forgetSchemaMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DROP TABLE schema_migrations`); err != nil {
		t.Fatal(err)
	}
}
