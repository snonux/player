package service

import (
	"math"
	"testing"

	"github.com/snonux/player/internal/model"
)

// B7: paging with client-supplied counts and offsets near MaxInt or below
// zero never panics and clips to the available items.
func TestSubsonicPagingClampsWithoutOverflow(t *testing.T) {
	songs := []model.Media{{ID: 1}, {ID: 2}, {ID: 3}}
	albums := []Album{{}, {}}
	people := []Artist{{}, {}}
	tests := []struct {
		offset, count, want int
	}{
		{1, math.MaxInt, 2},
		{math.MaxInt, math.MaxInt, 0},
		{math.MaxInt, 1, 0},
		{-5, 2, 2},
		{0, -1, 0},
		{math.MinInt, math.MaxInt, 3},
		{2, 5, 1},
	}
	for _, tt := range tests {
		if got := len(pageSongs(songs, nil, tt.offset, tt.count)); got != tt.want {
			t.Errorf("songs(%d, %d) = %d, want %d", tt.offset, tt.count, got, tt.want)
		}
		if got, want := len(pageAlbums(albums, tt.offset, tt.count)), min(tt.want, max(0, 2-max(tt.offset, 0))); got != want {
			t.Errorf("albums(%d, %d) = %d, want %d", tt.offset, tt.count, got, want)
		}
		if got, want := len(pageArtists(people, tt.offset, tt.count)), min(tt.want, max(0, 2-max(tt.offset, 0))); got != want {
			t.Errorf("artists(%d, %d) = %d, want %d", tt.offset, tt.count, got, want)
		}
	}
}
