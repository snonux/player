package service

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

type playlistRuleData struct {
	favorites map[int64]bool
	now       time.Time
}

// SetRules saves validated dynamic rules while retaining manual membership.
func (s *PlaylistService) SetRules(ctx context.Context, user int64, id string, rules *model.PlaylistRules) (*PlaylistView, error) {
	p, err := s.repo.GetPlaylist(ctx, user, id)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrNotFound
	}
	if !p.CanEdit {
		return nil, ErrForbidden
	}
	if p.Source != nil {
		return nil, &apiError{msg: "Stop following the source before changing rules.", status: 409}
	}
	if rules != nil {
		if err := validatePlaylistRules(*rules); err != nil {
			return nil, err
		}
	}
	ruleRepo, available := s.repo.(repository.PlaylistRulesRepo)
	if !available {
		return nil, &apiError{msg: "Smart playlists are unavailable.", status: 503}
	}
	ok, err := ruleRepo.SetPlaylistRules(ctx, user, id, rules)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &apiError{msg: "Playlist changed. Refresh before retrying.", status: 409}
	}
	return s.Get(ctx, user, id)
}

func validatePlaylistRules(r model.PlaylistRules) error {
	invalid := func() error {
		return &apiError{msg: "Choose all or any, 1–20 valid rules, a supported order and a song limit from 1 to 1000.", status: 400}
	}
	if (r.Match != "all" && r.Match != "any") || len(r.Conditions) < 1 || len(r.Conditions) > 20 || r.Limit < 1 || r.Limit > 1000 {
		return invalid()
	}
	switch r.Sort {
	case "title", "artist", "album", "year", "plays", "rating", "added", "last_played":
	default:
		return invalid()
	}
	for _, c := range r.Conditions {
		if !utf8.ValidString(c.Value) || utf8.RuneCountInString(c.Value) > 300 {
			return invalid()
		}
		switch c.Field {
		case "title", "artist", "album", "genre":
			if strings.TrimSpace(c.Value) == "" || (c.Operator != "contains" && c.Operator != "is") {
				return invalid()
			}
		case "year", "plays", "rating":
			n, err := strconv.Atoi(c.Value)
			if err != nil || n < 0 || n > 1000000000 || (c.Field == "rating" && n > 5) || (c.Field == "year" && n > 9999) || (c.Operator != "eq" && c.Operator != "gte" && c.Operator != "lte") {
				return invalid()
			}
		case "starred":
			if c.Operator != "is" || (c.Value != "true" && c.Value != "false") {
				return invalid()
			}
		case "added", "last_played":
			if c.Operator == "within_days" {
				n, err := strconv.Atoi(c.Value)
				if err != nil || n < 1 || n > 36500 {
					return invalid()
				}
			} else if c.Operator == "before" || c.Operator == "after" {
				if date, err := time.Parse("2006-01-02", c.Value); err != nil || date.Year() < 1 {
					return invalid()
				}
			} else {
				return invalid()
			}
		default:
			return invalid()
		}
	}
	return nil
}

func (s *PlaylistService) ruleData(ctx context.Context, user int64, songs map[int64]model.Media) (playlistRuleData, error) {
	data := playlistRuleData{favorites: map[int64]bool{}, now: time.Now().UTC()}
	favorites, err := s.source.ListMedia(ctx, user, MediaQueryFilter{Favorites: true})
	if err != nil {
		return data, err
	}
	for _, song := range favorites {
		data.favorites[song.ID] = true
	}
	// Ratings decorate catalog responses at the API boundary, so read the same
	// personal store here rather than relying on a caller's prior API decoration.
	if ratings, ok := s.repo.(repository.MusicRatingRepo); ok {
		values, err := ratings.ListMusicRatings(ctx, user, "song")
		if err != nil {
			return data, err
		}
		for id, song := range songs {
			song.Rating = values[strconv.FormatInt(id, 10)]
			songs[id] = song
		}
	}
	return data, nil
}

func smartPlaylist(p model.Playlist, songs map[int64]model.Media, data playlistRuleData) model.Playlist {
	if p.Rules == nil {
		return p
	}
	matches := make([]model.Media, 0)
	for _, song := range songs {
		matched := p.Rules.Match == "all"
		for _, c := range p.Rules.Conditions {
			yes := playlistRuleMatches(song, c, data)
			if p.Rules.Match == "all" && !yes {
				matched = false
				break
			}
			if p.Rules.Match == "any" && yes {
				matched = true
				break
			}
		}
		if matched {
			matches = append(matches, song)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		order := compareRuleSongs(matches[i], matches[j], p.Rules.Sort)
		if order == 0 {
			return matches[i].ID < matches[j].ID
		}
		if p.Rules.Descending {
			return order > 0
		}
		return order < 0
	})
	if len(matches) > p.Rules.Limit {
		matches = matches[:p.Rules.Limit]
	}
	p.Entries = make([]model.PlaylistEntry, 0, len(matches))
	for _, song := range matches {
		p.Entries = append(p.Entries, model.PlaylistEntry{ID: fmt.Sprintf("smart_%s_%d", p.ID, song.ID), MediaID: song.ID})
	}
	return p
}

func playlistRuleMatches(song model.Media, c model.PlaylistCondition, data playlistRuleData) bool {
	switch c.Field {
	case "title", "artist", "album", "genre":
		values := ruleText(song, c.Field)
		for _, value := range values {
			a, b := strings.ToLower(strings.TrimSpace(value)), strings.ToLower(strings.TrimSpace(c.Value))
			if (c.Operator == "is" && a == b) || (c.Operator == "contains" && strings.Contains(a, b)) {
				return true
			}
		}
		return false
	case "starred":
		return data.favorites[song.ID] == (c.Value == "true")
	case "year", "plays", "rating":
		actual := ruleNumber(song, c.Field)
		expected, _ := strconv.Atoi(c.Value)
		switch c.Operator {
		case "eq":
			return actual == expected
		case "gte":
			return actual >= expected
		case "lte":
			return actual <= expected
		}
	case "added", "last_played":
		actual := song.CreatedAt
		if c.Field == "last_played" {
			if song.LastPlayedAt == nil {
				return false
			}
			actual = *song.LastPlayedAt
		}
		if actual.IsZero() {
			return false
		}
		if c.Operator == "within_days" {
			days, _ := strconv.Atoi(c.Value)
			return !actual.Before(data.now.AddDate(0, 0, -days)) && !actual.After(data.now)
		}
		day, _ := time.Parse("2006-01-02", c.Value)
		if c.Operator == "before" {
			return actual.Before(day)
		}
		// After means strictly after the chosen UTC calendar date.
		return !actual.Before(day.AddDate(0, 0, 1))
	}
	return false
}
func ruleText(song model.Media, field string) []string {
	m := song.Music
	if m == nil {
		m = &model.MusicMetadata{}
	}
	switch field {
	case "title":
		if strings.TrimSpace(m.Title) != "" {
			return []string{m.Title}
		}
		return []string{strings.TrimSuffix(song.FileName, path.Ext(song.FileName))}
	case "artist":
		return append([]string{m.Artist}, m.Artists...)
	case "album":
		return []string{m.Album}
	case "genre":
		return m.Genres
	}
	return nil
}
func ruleNumber(song model.Media, field string) int {
	switch field {
	case "year":
		if song.Music != nil {
			return song.Music.Year
		}
	case "plays":
		return song.PersonalPlayCount
	case "rating":
		return song.Rating
	}
	return 0
}
func compareRuleSongs(a, b model.Media, field string) int {
	switch field {
	case "year", "plays", "rating":
		return ruleNumber(a, field) - ruleNumber(b, field)
	case "added", "last_played":
		x, y := a.CreatedAt, b.CreatedAt
		if field == "last_played" {
			x, y = time.Time{}, time.Time{}
			if a.LastPlayedAt != nil {
				x = *a.LastPlayedAt
			}
			if b.LastPlayedAt != nil {
				y = *b.LastPlayedAt
			}
		}
		return x.Compare(y)
	default:
		return strings.Compare(strings.ToLower(strings.Join(ruleText(a, field), " ")), strings.ToLower(strings.Join(ruleText(b, field), " ")))
	}
}
