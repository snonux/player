package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"time"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/probe"
	"github.com/snonux/player/internal/repository"
)

// ProviderLyrics owns one explicitly configured, account-scoped lyric agent.
type ProviderLyrics struct {
	endpoint string
	store    repository.ProviderLyricsRepo
	client   *http.Client
	gate     chan struct{}
	memo     map[string]string // accessed only while gate is held; at most 128 entries
}

// NewProviderLyrics creates an optional adapter. Invalid URLs leave it disabled.
func NewProviderLyrics(endpoint string, store repository.ProviderLyricsRepo) *ProviderLyrics {
	u, err := url.Parse(endpoint)
	if err != nil || u.User != nil || u.Fragment != "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || store == nil {
		return nil
	}
	return &ProviderLyrics{endpoint: endpoint, store: store, gate: make(chan struct{}, 1), memo: make(map[string]string), client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (s *browseService) loadLyrics(ctx context.Context, media *model.Media, user int64, action string) *Lyrics {
	local := localLyrics(ctx, s.mediaRoot, media)
	p := s.lyricsProvider
	if p == nil || media.Type != model.MediaTypeAudio {
		return local
	}
	local.ProviderEnabled = true
	local.ProviderState = "unfetched"
	if action == "remove" {
		if err := p.store.DeleteProviderLyrics(ctx, user, media.ID); err != nil {
			local.ProviderState = "error"
		}
		return local
	}
	if len(local.Lines) > 0 {
		return local
	}
	if action != "fetch" {
		found, err := p.store.HasProviderLyrics(ctx, user, media.ID)
		if err != nil || !found {
			return local
		}
	}
	// Bound hashing and external work together, and refuse excess concurrent work.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case p.gate <- struct{}{}:
		defer func() { <-p.gate }()
	default:
		local.ProviderState = "busy"
		return local
	}
	identity, err := p.contentIdentity(ctx, s.mediaRoot, media)
	if err != nil {
		local.ProviderState = "unavailable"
		return local
	}
	cached, err := p.store.LoadProviderLyrics(ctx, user, media.ID, identity)
	if err == nil && len(cached) > 0 {
		if value, e := decodeProviderLyrics(cached); e == nil {
			local = value
			local.ProviderEnabled = true
			local.ProviderState = "ready"
			if len(local.Lines) == 0 {
				local.ProviderState = "empty"
			}
		}
	}
	if action != "fetch" {
		return local
	}
	fresh, fetchErr := p.fetch(ctx, media)
	// A provider response cannot displace newly added local lyrics or survive a
	// source replacement while the agent was working.
	current, err := s.helper.verifyAccess(ctx, media.ID, user)
	if err != nil {
		return &Lyrics{Lines: []LyricsLine{}}
	}
	if current.Type != model.MediaTypeAudio {
		return &Lyrics{Lines: []LyricsLine{}}
	}
	latest := localLyrics(ctx, s.mediaRoot, current)
	if len(latest.Lines) > 0 {
		latest.ProviderEnabled = true
		return latest
	}
	next, err := p.contentIdentity(ctx, s.mediaRoot, current)
	if err != nil || next != identity {
		return &Lyrics{Lines: []LyricsLine{}, ProviderEnabled: true, ProviderState: "unavailable"}
	}
	if fetchErr != nil {
		local.ProviderState = "error"
		return local
	}
	data, err := json.Marshal(fresh)
	if err != nil || p.store.SaveProviderLyrics(ctx, user, media.ID, identity, data) != nil {
		local.ProviderState = "error"
		return local
	}
	fresh.ProviderEnabled = true
	fresh.ProviderState = "ready"
	if len(fresh.Lines) == 0 {
		fresh.ProviderState = "empty"
	}
	return fresh
}

func (p *ProviderLyrics) fetch(ctx context.Context, media *model.Media) (*Lyrics, error) {
	u, err := url.Parse(p.endpoint)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if media.Music == nil || media.Music.Title == "" || media.Music.Artist == "" {
		return nil, fmt.Errorf("track tags required")
	}
	for key, value := range map[string]string{"title": media.Music.Title, "artist": media.Music.Artist, "album": media.Music.Album, "recording_mbid": media.Music.RecordingMBID} {
		if len(value) > 4096 {
			return nil, fmt.Errorf("track tags too long")
		}
		q.Set(key, value)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	response, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == 204 {
		return &Lyrics{Lines: []LyricsLine{}}, nil
	}
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("provider HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, probe.MaxLyricsBytes+1))
	if err != nil {
		return nil, err
	}
	return decodeProviderLyrics(data)
}

func decodeProviderLyrics(data []byte) (*Lyrics, error) {
	if len(data) > probe.MaxLyricsBytes {
		return nil, fmt.Errorf("lyrics too large")
	}
	var value Lyrics
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if len(value.Lines) > 16384 {
		return nil, fmt.Errorf("too many lyric lines")
	}
	last := float64(-1)
	for i := range value.Lines {
		line := &value.Lines[i]
		if line.Start != nil && (math.IsNaN(*line.Start) || math.IsInf(*line.Start, 0) || *line.Start < 0 || *line.Start > 1e9) {
			return nil, fmt.Errorf("invalid lyric timing")
		}
		if value.Synced && (line.Start == nil || *line.Start < last) {
			return nil, fmt.Errorf("unordered lyrics")
		}
		if line.Start != nil {
			last = *line.Start
		}
		sanitizeRichLine(line)
	}
	if !validLyricLanguage(value.Language) {
		value.Language = ""
	}
	boundRichLyrics(&value)
	value.Source = "provider"
	value.ProviderEnabled = false
	value.ProviderState = ""
	if value.Lines == nil {
		value.Lines = []LyricsLine{}
	}
	return &value, nil
}

func (p *ProviderLyrics) contentIdentity(ctx context.Context, root string, media *model.Media) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path, err := resolveSourcePath(root, media.AbsPath)
	if err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realRoot, path)
	if err != nil {
		return "", err
	}
	dir, err := os.OpenRoot(realRoot)
	if err != nil {
		return "", err
	}
	defer func() { _ = dir.Close() }()
	file, err := dir.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() > 512*1024*1024 {
		return "", fmt.Errorf("unavailable source")
	}
	tags, _ := json.Marshal(media.Music)
	stamp := fmt.Sprintf("%s:%d:%v:%s:%s", path, before.Size(), before.Sys(), tags, p.endpoint)
	if cached := p.memo[stamp]; cached != "" {
		return cached, nil
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(p.endpoint + "\x00" + string(tags) + "\x00"))
	buf := make([]byte, 64*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, e := file.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > 512*1024*1024 {
				return "", fmt.Errorf("source too large")
			}
			_, _ = hash.Write(buf[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", e
		}
	}
	after, err := file.Stat()
	if err != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || changedFileCheckpoint(before, after) {
		return "", fmt.Errorf("source changed")
	}
	identity := fmt.Sprintf("%x", hash.Sum(nil))
	if len(p.memo) >= 128 {
		clear(p.memo)
	}
	p.memo[fmt.Sprintf("%s:%d:%v:%s:%s", path, after.Size(), after.Sys(), tags, p.endpoint)] = identity
	return identity, nil
}

// Ignore atime (our read may update it), but detect in-place edits restoring
// mtime and inode replacement while hashing. Sys field names differ by OS.
func changedFileCheckpoint(before, after os.FileInfo) bool {
	if !os.SameFile(before, after) {
		return true
	}
	a, b := reflect.ValueOf(before.Sys()), reflect.ValueOf(after.Sys())
	if a.Kind() != reflect.Pointer || b.Kind() != reflect.Pointer || a.IsNil() || b.IsNil() {
		return false
	}
	a, b = a.Elem(), b.Elem()
	if a.Kind() != reflect.Struct || b.Kind() != reflect.Struct {
		return false
	}
	for _, field := range []string{"Ctim", "Ctimespec", "Ctime", "Ctimensec"} {
		x, y := a.FieldByName(field), b.FieldByName(field)
		if x.IsValid() && y.IsValid() && !reflect.DeepEqual(x.Interface(), y.Interface()) {
			return true
		}
	}
	return false
}
