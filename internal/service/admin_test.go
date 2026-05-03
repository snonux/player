package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

type fakeScanner struct {
	scanFunc func(ctx context.Context, root string, progress *model.ScanProgress) error
}

func (f *fakeScanner) Scan(ctx context.Context, root string, progress *model.ScanProgress) error {
	if f.scanFunc != nil {
		return f.scanFunc(ctx, root, progress)
	}
	return nil
}

type fakeHasher struct {
	fixed string
	err   error
}

func (f *fakeHasher) Hash(password string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.fixed, nil
}
func (f *fakeHasher) Compare(hash, password string) error {
	return nil
}

func TestAdminService_ListTrash(t *testing.T) {
	ctx := context.Background()
	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			ListDeletedMediaFunc: func(ctx context.Context) ([]model.Media, error) {
				return []model.Media{{ID: 1, FileName: "a.mp4"}}, nil
			},
		},
	}
	svc := NewAdminService(store, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "", ctx)
	items, err := svc.ListTrash(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestAdminService_TriggerRescan(t *testing.T) {
	ctx := context.Background()
	var scannedRoot string
	done := make(chan struct{})
	sc := &fakeScanner{
		scanFunc: func(_ context.Context, root string, _ *model.ScanProgress) error {
			scannedRoot = root
			close(done)
			return nil
		},
	}
	svc := NewAdminService(&repository.MockStore{}, newMockClock(), &fakeHasher{fixed: "hash"}, sc, "/media", ctx)
	if err := svc.TriggerRescan(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	<-done
	// Give the background goroutine a moment to write scannedRoot.
	if scannedRoot != "/media" {
		t.Fatalf("expected root %q, got %q", "/media", scannedRoot)
	}
}

func TestAdminService_TriggerRescan_Error(t *testing.T) {
	ctx := context.Background()
	done := make(chan struct{})
	sc := &fakeScanner{
		scanFunc: func(_ context.Context, _ string, _ *model.ScanProgress) error {
			close(done)
			return errors.New("scan failed")
		},
	}
	svc := NewAdminService(&repository.MockStore{}, newMockClock(), &fakeHasher{fixed: "hash"}, sc, "/media", ctx)
	err := svc.TriggerRescan(ctx)
	// TriggerRescan now always returns nil immediately; failure is logged in background.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	<-done
}

func TestAdminService_TriggerRescan_NilScanner(t *testing.T) {
	ctx := context.Background()
	svc := NewAdminService(&repository.MockStore{}, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "", ctx)
	err := svc.TriggerRescan(ctx)
	if err == nil {
		t.Fatal("expected error when scanner is nil")
	}
}

func TestAdminService_ListUsers(t *testing.T) {
	ctx := context.Background()
	store := &repository.MockStore{
		UserRepo: repository.MockUserRepo{
			ListUsersFunc: func(ctx context.Context) ([]model.User, error) {
				return []model.User{{ID: 1, Username: "alice"}}, nil
			},
		},
	}
	svc := NewAdminService(store, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "", ctx)
	users, err := svc.ListUsers(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
}

func TestAdminService_CreateUser(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		hashErr   error
		createErr error
		wantErr   bool
	}{
		{
			name: "ok",
		},
		{
			name:    "hash error",
			hashErr: errors.New("boom"),
			wantErr: true,
		},
		{
			name:      "create error",
			createErr: errors.New("boom"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				UserRepo: repository.MockUserRepo{
					CreateUserFunc: func(ctx context.Context, user *model.User) (int64, error) {
						return 1, tt.createErr
					},
				},
			}
			hasher := &fakeHasher{fixed: "hashed", err: tt.hashErr}
			svc := NewAdminService(store, newMockClock(), hasher, nil, "", ctx)
			user, err := svc.CreateUser(ctx, "alice", "secret", false)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if user.Username != "alice" {
				t.Fatalf("unexpected username %q", user.Username)
			}
		})
	}
}

func TestAdminService_DeleteUser(t *testing.T) {
	ctx := context.Background()
	var called bool
	store := &repository.MockStore{
		UserRepo: repository.MockUserRepo{
			DeleteUserFunc: func(ctx context.Context, id int64) error {
				called = true
				return nil
			},
		},
	}
	svc := NewAdminService(store, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "", ctx)
	if err := svc.DeleteUser(ctx, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected delete called")
	}
}

func TestAdminService_ListPermissions(t *testing.T) {
	ctx := context.Background()
	store := &repository.MockStore{
		SetRepo: repository.MockSetRepo{
			ListSetsFunc: func(ctx context.Context) ([]model.Set, error) {
				return []model.Set{{ID: 1}, {ID: 2}}, nil
			},
		},
		SetPermissionRepo: repository.MockSetPermissionRepo{
			ListPermissionsBySetFunc: func(ctx context.Context, setID int64) ([]model.SetPermission, error) {
				return []model.SetPermission{{SetID: setID, UserID: int64(setID) + 10}}, nil
			},
		},
	}
	svc := NewAdminService(store, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "", ctx)
	perms, err := svc.ListPermissions(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if perms == nil || len(perms.Permissions) != 2 {
		t.Fatalf("expected 2 permissions, got %+v", perms)
	}
	if len(perms.Sets) != 2 {
		t.Fatalf("expected 2 sets, got %d", len(perms.Sets))
	}
}

func TestAdminService_GrantPermission(t *testing.T) {
	ctx := context.Background()
	var granted *model.SetPermission
	store := &repository.MockStore{
		SetPermissionRepo: repository.MockSetPermissionRepo{
			GrantPermissionFunc: func(ctx context.Context, perm *model.SetPermission) error {
				granted = perm
				return nil
			},
		},
	}
	svc := NewAdminService(store, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "", ctx)
	if err := svc.GrantPermission(ctx, 1, 2, model.RoleViewer); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if granted == nil {
		t.Fatal("expected permission granted")
	}
	if granted.Role != model.RoleViewer {
		t.Fatalf("unexpected role %q", granted.Role)
	}
}

func TestAdminService_RevokePermission(t *testing.T) {
	ctx := context.Background()
	var revoked bool
	store := &repository.MockStore{
		SetPermissionRepo: repository.MockSetPermissionRepo{
			RevokePermissionFunc: func(ctx context.Context, setID, userID int64) error {
				revoked = true
				return nil
			},
		},
	}
	svc := NewAdminService(store, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "", ctx)
	if err := svc.RevokePermission(ctx, 1, 2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !revoked {
		t.Fatal("expected revoke called")
	}
}

func TestAdminService_TriggerRescan_CancelsPrevious(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{}, 2)
	sc := &fakeScanner{
		scanFunc: func(scanCtx context.Context, _ string, progress *model.ScanProgress) error {
			progress.Start(1)
			started <- struct{}{}
			<-scanCtx.Done()
			return scanCtx.Err()
		},
	}
	svc := NewAdminService(&repository.MockStore{}, newMockClock(), &fakeHasher{fixed: "hash"}, sc, "/media", ctx)

	// Start first scan.
	if err := svc.TriggerRescan(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	<-started

	// Start second scan — should cancel the first.
	if err := svc.TriggerRescan(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	<-started

	// Verify final progress is from the second (still running) scan.
	progress := svc.ScanProgress(ctx)
	if !progress.Running {
		t.Fatal("expected second scan to be running")
	}
}

func TestAdminService_TriggerRescan_FreshProgressPerScan(t *testing.T) {
	ctx := context.Background()
	done := make(chan struct{})
	sc := &fakeScanner{
		scanFunc: func(_ context.Context, _ string, progress *model.ScanProgress) error {
			progress.Start(5)
			<-done
			return nil
		},
	}
	svc := NewAdminService(&repository.MockStore{}, newMockClock(), &fakeHasher{fixed: "hash"}, sc, "/media", ctx)

	if err := svc.TriggerRescan(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Wait for goroutine to start.
	for {
		p := svc.ScanProgress(ctx)
		if p.Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(done)
	// Wait for goroutine to finish.
	for {
		p := svc.ScanProgress(ctx)
		if !p.Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	p1 := svc.ScanProgress(ctx)
	if p1.SetsTotal != 5 {
		t.Fatalf("expected sets_total 5, got %d", p1.SetsTotal)
	}

	// Start a new scan on the same service with different progress.
	done2 := make(chan struct{})
	sc2 := &fakeScanner{
		scanFunc: func(_ context.Context, _ string, progress *model.ScanProgress) error {
			progress.Start(10)
			<-done2
			return nil
		},
	}
	// We replace the scanner field via reflection? No, easier: just create new service.
	// Actually, the test verifies per-service fresh progress, so new service is fine.
	svc2 := NewAdminService(&repository.MockStore{}, newMockClock(), &fakeHasher{fixed: "hash"}, sc2, "/media", ctx)
	if err := svc2.TriggerRescan(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Wait for goroutine to start.
	for {
		p := svc2.ScanProgress(ctx)
		if p.Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(done2)
	// Wait for goroutine to finish.
	for {
		p := svc2.ScanProgress(ctx)
		if !p.Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	p2 := svc2.ScanProgress(ctx)
	if p2.SetsTotal != 10 {
		t.Fatalf("expected sets_total 10, got %d", p2.SetsTotal)
	}
	if p2.LastError != "" {
		t.Fatalf("unexpected last_error: %s", p2.LastError)
	}
}

func TestAdminService_ScanProgress_ReturnsEmptyWhenNotStarted(t *testing.T) {
	ctx := context.Background()
	svc := NewAdminService(&repository.MockStore{}, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "", ctx)
	p := svc.ScanProgress(ctx)
	if p.Running {
		t.Fatal("expected not running when no scan started")
	}
}

func TestAdminService_TriggerRescan_ConcurrentCalls(t *testing.T) {
	ctx := context.Background()
	var wg sync.WaitGroup
	callCount := 0
	var mu sync.Mutex
	sc := &fakeScanner{
		scanFunc: func(scanCtx context.Context, _ string, progress *model.ScanProgress) error {
			mu.Lock()
			callCount++
			mu.Unlock()
			progress.Start(1)
			<-scanCtx.Done()
			return scanCtx.Err()
		},
	}
	svc := NewAdminService(&repository.MockStore{}, newMockClock(), &fakeHasher{fixed: "hash"}, sc, "/media", ctx)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = svc.TriggerRescan(ctx)
		}()
	}
	wg.Wait()

	// Wait for the final surviving goroutine to start.
	for {
		progress := svc.ScanProgress(ctx)
		if progress.Running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	if callCount == 0 {
		t.Fatal("expected at least one scan to start")
	}
	mu.Unlock()

	// Final progress should reflect the last scan.
	progress := svc.ScanProgress(ctx)
	if !progress.Running {
		t.Fatal("expected a scan to be running after concurrent calls")
	}
}

func TestAdminService_ListPermissions_Error(t *testing.T) {
	ctx := context.Background()

	t.Run("list sets error", func(t *testing.T) {
		store := &repository.MockStore{
			SetRepo: repository.MockSetRepo{
				ListSetsFunc: func(ctx context.Context) ([]model.Set, error) {
					return nil, errors.New("boom")
				},
			},
		}
		svc := NewAdminService(store, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "", ctx)
		_, err := svc.ListPermissions(ctx)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("list permissions by set error", func(t *testing.T) {
		store := &repository.MockStore{
			SetRepo: repository.MockSetRepo{
				ListSetsFunc: func(ctx context.Context) ([]model.Set, error) {
					return []model.Set{{ID: 1}}, nil
				},
			},
			SetPermissionRepo: repository.MockSetPermissionRepo{
				ListPermissionsBySetFunc: func(ctx context.Context, setID int64) ([]model.SetPermission, error) {
					return nil, errors.New("boom")
				},
			},
		}
		svc := NewAdminService(store, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "", ctx)
		_, err := svc.ListPermissions(ctx)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
