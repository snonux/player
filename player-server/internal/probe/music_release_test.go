package probe

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/snonux/player/internal/ffsafe/ffsafetest"
	"github.com/snonux/player/internal/model"
)

func TestMusicReleaseMetadata(t *testing.T) {
	tags := musicTags(map[string]string{
		"DATE": "2024", "release_date": "2025-04", "releasedate": "2026",
		"originaldate": "1987", "recordingdate": "1986-12-03", "albumversion": "Live 東京",
		"releasetype": "album\x00live", "label": "A; B\x00C / D", "catalognumber": "CAT/1\x00CAT,2",
		"media": "CD\x00Digital", "mood": "Calm\x00Bright", "grouping": "Suite / One",
		"comment": "<b>Exact</b>\n第二行", "producer": "A; B\x00C", "credit:  Tape OP ": "D/E\x00F, G",
	})
	m := parseMusicTags(tags)
	if m.Date != "2024" || m.Year != 2024 || m.ReleaseDate != "2025-04" || m.OriginalDate != "1987" || m.RecordingDate != "1986-12-03" || m.ReleaseVersion != "Live 東京" || m.Grouping != "Suite / One" || m.Comment != "<b>Exact</b>\n第二行" {
		t.Fatalf("release fields %+v", m)
	}
	if !reflect.DeepEqual(m.Labels, []string{"A; B", "C / D"}) || !reflect.DeepEqual(m.ReleaseTypes, []string{"album", "live"}) || !reflect.DeepEqual(m.CatalogNumbers, []string{"CAT/1", "CAT,2"}) || !reflect.DeepEqual(m.Media, []string{"CD", "Digital"}) || !reflect.DeepEqual(m.Moods, []string{"Calm", "Bright"}) {
		t.Fatalf("structured values %+v", m)
	}
	want := []model.MusicCredit{{Role: "producer", Names: []string{"A; B", "C"}}, {Role: "tape op", Names: []string{"D/E", "F, G"}}}
	if !reflect.DeepEqual(m.Credits, want) {
		t.Fatalf("credits %+v", m.Credits)
	}
	for _, tags := range []map[string]string{{"date": "Spring release", "year": "2024"}, {"year": "1999"}, {"originalyear": "1970"}, {}} {
		m := parseMusicTags(tags)
		if m.RecordingDate != "" {
			t.Fatalf("invented recording date %+v", m)
		}
		if tags["originalyear"] == "" && m.OriginalDate != "" {
			t.Fatalf("invented original date %+v", m)
		}
		if tags["date"] != "" && m.ReleaseDate != tags["date"] {
			t.Fatalf("fallback %+v", m)
		}
	}
}

func TestReleaseVorbisBoundaries(t *testing.T) {
	tags, err := vorbisComments(commentBlock(t, "LABEL=A; B", "LABEL=東京", "RELEASETYPE=album", "RELEASETYPE=live", "MOOD=Calm", "MOOD=Bright", "PRODUCER=A / B", "PRODUCER=C", "CREDIT:Tape Op=D; E", "CREDIT:Tape Op=F", "UNRELATED=not indexed"))
	if err != nil {
		t.Fatal(err)
	}
	m := parseMusicTags(tags)
	if !reflect.DeepEqual(m.Labels, []string{"A; B", "東京"}) || !reflect.DeepEqual(m.Moods, []string{"Calm", "Bright"}) || len(m.Credits) != 2 || !reflect.DeepEqual(m.Credits[1].Names, []string{"D; E", "F"}) || tags["unrelated"] != "" {
		t.Fatalf("%+v", m)
	}
}

func TestMusicReleaseRealContainers(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	for _, format := range []struct{ ext, codec string }{{"mp3", "libmp3lame"}, {"flac", "flac"}, {"m4a", "aac"}, {"opus", "libopus"}} {
		t.Run(format.ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "release."+format.ext)
			args := []string{"-f", "lavfi", "-i", "sine=duration=0.1", "-c:a", format.codec, "-metadata", "discsubtitle=Encore 東京", "-metadata", "compilation=1", "-metadata", "release_date=2024-05", "-metadata", "originaldate=1979", "-metadata", "recordingdate=1978-11-03", "-metadata", "albumversion=Remastered", "-metadata", "label=Label 東京", "-metadata", "credit:Tape Op=A / B"}
			if format.ext == "m4a" {
				args = append(args, "-movflags", "+use_metadata_tags")
			}
			ffsafetest.Generate(t, path, args...)
			got, err := NewFFProber().Probe(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			m := got.Music
			if m.DiscSubtitle != "Encore 東京" || !m.Compilation || m.ReleaseDate != "2024-05" || m.OriginalDate != "1979" || m.RecordingDate != "1978-11-03" || m.ReleaseVersion != "Remastered" || !reflect.DeepEqual(m.Labels, []string{"Label 東京"}) || len(m.Credits) != 1 || m.Credits[0].Names[0] != "A / B" {
				t.Fatalf("release %+v", m)
			}
		})
	}
}
