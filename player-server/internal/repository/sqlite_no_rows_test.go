package repository

import (
	"context"
	"testing"
)

func TestSQLite_NoRows_ReturnsNil(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	defer s.Close()

	t.Run("GetUserByID", func(t *testing.T) {
		u, err := s.GetUserByID(ctx, 9999)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if u != nil {
			t.Fatalf("expected nil, got %+v", u)
		}
	})

	t.Run("GetUserByUsername", func(t *testing.T) {
		u, err := s.GetUserByUsername(ctx, "nobody")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if u != nil {
			t.Fatalf("expected nil, got %+v", u)
		}
	})

	t.Run("GetByHash", func(t *testing.T) {
		token, err := s.GetByHash(ctx, "missing")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if token != nil {
			t.Fatalf("expected nil, got %+v", token)
		}
	})

	t.Run("GetSetByID", func(t *testing.T) {
		st, err := s.GetSetByID(ctx, 9999)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if st != nil {
			t.Fatalf("expected nil, got %+v", st)
		}
	})

	t.Run("GetMediaByID", func(t *testing.T) {
		m, err := s.GetMediaByID(ctx, 9999)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if m != nil {
			t.Fatalf("expected nil, got %+v", m)
		}
	})

	t.Run("GetTagByID", func(t *testing.T) {
		tag, err := s.GetTagByID(ctx, 9999)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if tag != nil {
			t.Fatalf("expected nil, got %+v", tag)
		}
	})

	t.Run("GetTagByName", func(t *testing.T) {
		tag, err := s.GetTagByName(ctx, "missing")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if tag != nil {
			t.Fatalf("expected nil, got %+v", tag)
		}
	})

	t.Run("GetPermission", func(t *testing.T) {
		p, err := s.GetPermission(ctx, 9999, 9999)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if p != nil {
			t.Fatalf("expected nil, got %+v", p)
		}
	})

	t.Run("GetNote", func(t *testing.T) {
		n, err := s.GetNote(ctx, 9999, 9999)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if n != nil {
			t.Fatalf("expected nil, got %+v", n)
		}
	})

	t.Run("GetProgress", func(t *testing.T) {
		p, err := s.GetProgress(ctx, 9999, 9999)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if p != nil {
			t.Fatalf("expected nil, got %+v", p)
		}
	})

	t.Run("GetAccumulator", func(t *testing.T) {
		a, err := s.GetAccumulator(ctx, "nope", 9999)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if a != nil {
			t.Fatalf("expected nil, got %+v", a)
		}
	})

	t.Run("GetSessionByID", func(t *testing.T) {
		sess, err := s.GetSessionByID(ctx, "nope")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if sess != nil {
			t.Fatalf("expected nil, got %+v", sess)
		}
	})

	t.Run("GetShareByToken", func(t *testing.T) {
		sh, err := s.GetShareByToken(ctx, "nope")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if sh != nil {
			t.Fatalf("expected nil, got %+v", sh)
		}
	})
}
