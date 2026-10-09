package repository

import (
	"fmt"
	"strings"
)

// Per-connection settings.
//
// SQLite keeps "PRAGMA foreign_keys" and "PRAGMA busy_timeout" per
// connection, and database/sql replaces a pooled connection whenever it
// discards one (a driver error, a query cancelled through its request
// context). A pragma set once with db.Exec is therefore lost on the
// replacement: foreign keys went back to OFF, every ON DELETE CASCADE in the
// schema silently stopped, and deleting a user left that user's sessions
// behind and valid.
//
// connectionDSN puts the settings into the DSN instead, where the driver
// applies them to every connection it opens.

// connectionPragmas are applied by the driver to each new connection.
var connectionPragmas = []string{
	"foreign_keys(1)",
	fmt.Sprintf("busy_timeout(%d)", sqliteBusyTimeoutMS),
}

// connectionDSN returns dsn with the options every connection of the store
// needs: UTC timestamps (see time_utc.go), foreign key enforcement and the
// busy timeout. An option the caller already set is left alone.
func connectionDSN(dsn string) string {
	dsn = utcTimesDSN(dsn)
	for _, pragma := range connectionPragmas {
		name := pragma[:strings.Index(pragma, "(")]
		if strings.Contains(dsn, "_pragma="+name+"(") {
			continue
		}
		dsn = appendDSNOption(dsn, "_pragma="+pragma)
	}
	return dsn
}

// appendDSNOption adds one query option to a DSN that may or may not have a
// query part yet.
func appendDSNOption(dsn, option string) string {
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + option
}
