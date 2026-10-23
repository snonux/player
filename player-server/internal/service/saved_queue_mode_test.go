package service

import (
	"github.com/snonux/player/internal/model"
	"testing"
)

func TestSavedQueueShuffleMode(t *testing.T) {
	q := model.SavedQueue{MutationID: "test", Repeat: "off"}
	for _, mode := range []string{"", "tracks", "albums"} {
		q.ShuffleMode = mode
		if err := validateSavedQueue(q, 0); err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
	}
	q.ShuffleMode = "unknown"
	if err := validateSavedQueue(q, 0); err == nil {
		t.Fatal("invalid mode accepted")
	}
}
