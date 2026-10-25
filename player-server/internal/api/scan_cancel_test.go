package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/snonux/player/internal"
	"github.com/snonux/player/internal/auth"
	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/service"
)

func TestScanCancelAuthorizationAndAliases(t *testing.T) {
	for _, prefix := range []string{"/api/", "/api/v1/"} {
		for _, role := range []string{"anonymous", "listener", "admin"} {
			t.Run(prefix+role, func(t *testing.T) {
				store := buildSessionStore(1)
				sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
				authSvc := &service.MockAuthService{
					CountUsersFunc: func(context.Context) (int, error) { return 1, nil },
					GetUserByIDFunc: func(context.Context, int64) (*model.User, error) {
						return &model.User{ID: 1, IsAdmin: role == "admin"}, nil
					},
				}
				called := false
				admin := &service.MockAdminService{CancelScanFunc: func(context.Context) error { called = true; return nil }}
				srv := newTestServer(t, store, nil, sm, &internal.Config{SessionTimeoutHours: 24}, nil, nil, nil, nil, nil, nil, admin, nil, authSvc, nil)
				req := httptest.NewRequest(http.MethodPost, prefix+"admin/scan-cancel", nil)
				want := http.StatusUnauthorized
				if role != "anonymous" {
					req.AddCookie(addSessionCookie(t, store, sm, 1))
					want = http.StatusForbidden
				}
				if role == "admin" {
					want = http.StatusOK
				}
				rr := httptest.NewRecorder()
				srv.ServeHTTP(rr, req)
				if rr.Code != want || called != (role == "admin") {
					t.Fatalf("status=%d want=%d called=%v", rr.Code, want, called)
				}
			})
		}
	}
}
