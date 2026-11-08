package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// queryer is satisfied by *sql.DB and *sql.Tx.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// queryList runs query and maps every row through scan. label prefixes the
// query error, matching the "list <things>: %w" style used across the
// repository. An empty result is a nil slice, like the loops it replaces.
func queryList[T any](ctx context.Context, q queryer, label string, scan func(*sql.Rows) (T, error), query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// byValue adapts a pointer-returning row scanner (scanShare, scanTag, …) to
// the value form queryList collects.
func byValue[T any](scan func(sqlScanner) (*T, error)) func(*sql.Rows) (T, error) {
	return func(rows *sql.Rows) (T, error) {
		v, err := scan(rows)
		if err != nil {
			var zero T
			return zero, err
		}
		return *v, nil
	}
}
