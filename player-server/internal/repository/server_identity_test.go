package repository

import (
	"context"
	"path/filepath"
	"testing"
)

func TestServerIdentityPersistsAndSeparatesDatabases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ServerIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 {
		t.Fatalf("identity length = %d", len(first))
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	second, err := reopened.ServerIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("identity changed on reopen")
	}
	separate, err := Open(filepath.Join(t.TempDir(), "other.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = separate.Close() }()
	other, err := separate.ServerIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first == other {
		t.Fatal("separate databases share an identity")
	}
}
