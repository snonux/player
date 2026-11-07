package service

import (
	"context"
	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/repository"
	"testing"
	"time"
)

func TestPodcastRetentionRunsAtStartupAndTick(t *testing.T) {
	calls := 0
	worker := NewGCWorker(&repository.MockStore{}, &clock.MockClock{T: time.Now()}, t.TempDir(), time.Hour, nil).
		WithPodcastRetention(func(context.Context) error { calls++; return nil })
	tick := make(chan time.Time)
	worker.tickCh = tick
	worker.runDoneCh = make(chan struct{}, 1)
	worker.Start()
	if calls != 1 {
		t.Fatal("startup retention missing")
	}
	tick <- time.Now()
	<-worker.runDoneCh
	worker.Stop()
	if calls != 2 {
		t.Fatal("tick retention missing")
	}
	worker.Start()
	if calls != 2 {
		t.Fatal("stopped worker restarted retention")
	}
}
