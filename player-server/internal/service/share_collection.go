package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/snonux/player/internal/model"
)

// CreateCollectionShare captures ordered occurrences, preserving repeated songs.
func (s *shareService) CreateCollectionShare(ctx context.Context, userID int64, title string, ids []int64, expiresAt time.Time, maxUses *int) (*model.Share, error) {
	title = strings.TrimSpace(title)
	if len(ids) == 0 || len(ids) > 1000 || title == "" || utf8.RuneCountInString(title) > 200 || !expiresAt.After(s.clock.Now()) || (maxUses != nil && *maxUses < 1) {
		return nil, ErrInvalidCollection
	}
	for _, id := range ids {
		media, err := s.helper.verifyAccess(ctx, id, userID)
		if err != nil {
			return nil, err
		}
		if media.Type != model.MediaTypeAudio {
			return nil, ErrInvalidCollection
		}
	}
	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("generate collection token: %w", err)
	}
	share := &model.Share{Token: token, CreatedBy: userID, CreatedAt: s.clock.Now(), ExpiresAt: expiresAt, MaxUses: maxUses, CollectionTitle: title, MediaIDs: append([]int64(nil), ids...)}
	if err := s.store.CreateShare(ctx, share); err != nil {
		return nil, fmt.Errorf("create collection share: %w", err)
	}
	return share, nil
}

// ResolveSharedItem resolves only an occurrence inside the token's snapshot.
func (s *shareService) ResolveSharedItem(ctx context.Context, access ShareAccess) (*model.Media, error) {
	share, err := s.usableShare(ctx, access.Token, access.Credentials...)
	if err != nil {
		return nil, err
	}
	if len(share.MediaIDs) == 0 {
		if access.Item != 0 {
			return nil, ErrShareNotFound
		}
		return s.ResolveSharedMedia(ctx, access.Token, access.Credentials...)
	}
	if access.Item < 0 || access.Item >= len(share.MediaIDs) {
		return nil, ErrShareNotFound
	}
	media, err := s.helper.verifyAccess(ctx, share.MediaIDs[access.Item], share.CreatedBy)
	if err != nil {
		return nil, ErrShareNotFound
	}
	if media.Type != model.MediaTypeAudio {
		return nil, ErrShareNotFound
	}
	return media, nil
}

func (s *shareService) getSharedCollection(ctx context.Context, access ShareAccess, share *model.Share) (*GetSharedMediaResult, error) {
	items := make([]GetSharedMediaResult, 0, len(share.MediaIDs))
	for i := range share.MediaIDs {
		itemAccess := access
		itemAccess.Item = i
		media, err := s.ResolveSharedItem(ctx, itemAccess)
		if err != nil {
			items = append(items, GetSharedMediaResult{Media: &SharedMediaView{ID: int64(i + 1), FileName: "Unavailable song", Type: model.MediaTypeAudio}, Unavailable: true})
			continue
		}
		base := fmt.Sprintf("/s/%s/items/%d", access.Token, i)
		public := sharedMediaView(media)
		public.ID = int64(i + 1)
		// Collection guests need display tags only, not library identities or comments.
		if media.Music != nil {
			public.Music = &model.MusicMetadata{Title: media.Music.Title, Artist: media.Music.Artist, Album: media.Music.Album, Track: media.Music.Track, Disc: media.Music.Disc}
		}
		item := GetSharedMediaResult{Media: public, StreamURL: base + "/stream", PlaybackURL: base + "/stream", DownloadURL: base + "/download", HasThumb: media.ThumbnailPath != "", Transcoded: media.NeedsCompatStream()}
		if item.HasThumb {
			item.ThumbURL = base + "/thumbnail"
		}
		if item.Transcoded {
			item.PlaybackURL = base + "/compat"
		}
		items = append(items, item)
	}
	viewing, _, err := s.activeViewing(ctx, access.Token, access.Credentials...)
	if err != nil {
		return nil, err
	}
	if !access.Probe {
		viewing, err = s.EnsureShareViewing(ctx, access.Token, access.Credentials...)
		if err != nil {
			return nil, err
		}
	}
	result := items[0]
	result.Items = items
	result.CollectionTitle = share.CollectionTitle
	result.Description = share.Description
	result.Viewing = viewing
	return &result, nil
}

// GetSharedItemThumbnail returns artwork confined to an occurrence in the snapshot.
func (s *shareService) GetSharedItemThumbnail(ctx context.Context, access ShareAccess) (*FileResult, error) {
	media, err := s.ResolveSharedItem(ctx, access)
	if err != nil {
		return nil, err
	}
	if media.ThumbnailPath == "" {
		return nil, ErrMediaNotFound
	}
	return &FileResult{Path: media.ThumbnailPath}, nil
}
