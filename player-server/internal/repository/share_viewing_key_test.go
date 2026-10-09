package repository

import (
	"bytes"
	"context"
	"path/filepath"
	"sync"
	"testing"
)

// openKeyStore opens the database at path and closes it with the test.
func openKeyStore(t *testing.T, path string) *SQLite {
	t.Helper()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// The key is created exactly once, also when its first users race: every
// caller gets the same 32 random bytes.
func TestShareViewingKey_CreatedOnce(t *testing.T) {
	store := openKeyStore(t, filepath.Join(t.TempDir(), "media.db"))

	const callers = 8
	keys := make([][]byte, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, err := store.ShareViewingKey(context.Background())
			if err != nil {
				t.Errorf("caller %d: %v", i, err)
			}
			keys[i] = key
		}()
	}
	wg.Wait()

	if len(keys[0]) != shareViewingKeyBytes || bytes.Equal(keys[0], make([]byte, shareViewingKeyBytes)) {
		t.Fatalf("key = %x, want %d random bytes", keys[0], shareViewingKeyBytes)
	}
	for i, key := range keys {
		if !bytes.Equal(key, keys[0]) {
			t.Fatalf("caller %d got a different key", i)
		}
	}
}

// The key survives a reopen of the database (a server restart), and every
// database has its own.
func TestShareViewingKey_PersistentPerDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "media.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ShareViewingKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := openKeyStore(t, path).ShareViewingKey(ctx)
	if err != nil || !bytes.Equal(again, first) {
		t.Fatalf("key after reopen differs (err=%v)", err)
	}
	other, err := openKeyStore(t, filepath.Join(t.TempDir(), "other.db")).ShareViewingKey(ctx)
	if err != nil || bytes.Equal(other, first) {
		t.Fatalf("a second database got the same key (err=%v)", err)
	}
}
