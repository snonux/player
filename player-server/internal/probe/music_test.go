package probe

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"codeberg.org/snonux/player/internal/ffsafe/ffsafetest"
	"codeberg.org/snonux/player/internal/model"
)

func TestMusicTags(t *testing.T) {
	cases := []struct {
		name string
		tags map[string]string
		want model.MusicMetadata
	}{
		{"complete Unicode", map[string]string{"TITLE": " Café Ñoño ", "ARTIST": "Aurora feat. Kei; A/B", "ALBUM_ARTIST": "Ünïcødé", "ALBUM": "東京 Nights", "TRACK": "02/12", "DISCNUMBER": "2/3", "DATE": "2022-11-03", "GENRE": "Électronique"}, model.MusicMetadata{Title: "Café Ñoño", Artist: "Aurora feat. Kei; A/B", Artists: []string{"Aurora feat. Kei; A/B"}, AlbumArtist: "Ünïcødé", AlbumArtists: []string{"Ünïcødé"}, Album: "東京 Nights", Track: 2, TrackTotal: 12, Disc: 2, DiscTotal: 3, Date: "2022-11-03", Year: 2022, Genres: []string{"Électronique"}}},
		{"missing and invalid", map[string]string{"title": " \t ", "track": "-2/no", "disc": "banana", "date": "unknown"}, model.MusicMetadata{Date: "unknown"}},
		{"structured values", map[string]string{"artist": "A\x00B\x00A", "albumartist": "C\x00D", "genre": "Jazz\x00Blues"}, model.MusicMetadata{Artist: "A, B", Artists: []string{"A", "B"}, AlbumArtist: "C, D", AlbumArtists: []string{"C", "D"}, Genres: []string{"Jazz", "Blues"}}},
		{"non-year date retains separate year", map[string]string{"date": "Spring release", "year": "2024"}, model.MusicMetadata{Date: "Spring release", Year: 2024}},
		{"date year wins conflict", map[string]string{"date": "2023-03-01", "year": "2024"}, model.MusicMetadata{Date: "2023-03-01", Year: 2023}},
		{"display plus explicit artists", map[string]string{"artist": "A & B", "artists": "A\x00B"}, model.MusicMetadata{Artist: "A & B", Artists: []string{"A", "B"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseMusicTags(musicTags(tc.tags))
			if !reflect.DeepEqual(*got, tc.want) {
				t.Fatalf("got %+v; want %+v", *got, tc.want)
			}
		})
	}
}

func TestMusicProbeStreamTags(t *testing.T) {
	data := []byte(`{"format":{"tags":{"title":"Container"}},"streams":[{"codec_type":"video","tags":{"artist":"Cover"}},{"codec_type":"audio","tags":{"TITLE":"Stream","ARTIST":"Singer","album":"Record"}}]}`)
	m, err := parseFFprobeOutput(data, true)
	if err != nil || m.Music.Title != "Container" || m.Music.Artist != "Singer" || m.Music.Album != "Record" {
		t.Fatalf("%+v %v", m, err)
	}
	video, err := parseFFprobeOutput(data, false)
	if err != nil || video.Music != nil {
		t.Fatalf("non-audio tags: %+v %v", video, err)
	}
}

func commentBlock(t *testing.T, entries ...string) []byte {
	t.Helper()
	var b bytes.Buffer
	put := func(n int) {
		if err := binary.Write(&b, binary.LittleEndian, uint32(n)); err != nil {
			t.Fatal(err)
		}
	}
	put(0)
	put(len(entries))
	for _, entry := range entries {
		put(len(entry))
		b.WriteString(entry)
	}
	return b.Bytes()
}

func TestVorbisCommentsBoundariesAndMalformed(t *testing.T) {
	block := commentBlock(t, "ARTIST=A; B", "ARTIST=C", "GENRE=Jazz", "GENRE=Blues")
	tags, err := vorbisComments(block)
	if err != nil || tags["artist"] != "A; B\x00C" || tags["genre"] != "Jazz\x00Blues" {
		t.Fatalf("%v %v", tags, err)
	}
	for n := 0; n < len(block); n++ {
		if _, err := vorbisComments(block[:n]); err == nil {
			t.Fatalf("truncated at %d accepted", n)
		}
	}
}

func TestMusicRealContainers(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	for _, codec := range []struct{ ext, encoder string }{{"mp3", "libmp3lame"}, {"flac", "flac"}, {"m4a", "aac"}, {"ogg", "libvorbis"}, {"opus", "libopus"}, {"wav", "pcm_s16le"}} {
		t.Run(codec.ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "unrelated-file."+codec.ext)
			ffsafetest.Generate(t, path, "-f", "lavfi", "-i", "sine=duration=0.1", "-c:a", codec.encoder, "-metadata", "title=Café Ñoño", "-metadata", "artist=A feat. B", "-metadata", "album_artist=Album Singer", "-metadata", "album=東京 Nights", "-metadata", "track=2/9", "-metadata", "disc=2/3", "-metadata", "date=2022-11-03", "-metadata", "genre=Électronique")
			m, err := NewFFProber().Probe(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if m.Music.Title != "Café Ñoño" || m.Music.Artist != "A feat. B" || m.Music.Album != "東京 Nights" || m.Music.Track != 2 || m.Music.Year != 2022 {
				t.Fatalf("tags: %+v", m.Music)
			}
			// WAV cannot carry disc/album artist in standard RIFF INFO.
			if codec.ext != "wav" && (m.Music.AlbumArtist != "Album Singer" || m.Music.Disc != 2) {
				t.Fatalf("tags: %+v", m.Music)
			}
			if codec.ext == "flac" {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				block := commentBlock(t, "TITLE=Separate Artists", "ARTIST=A; B", "ARTIST=C", "ALBUMARTIST=D", "ALBUMARTIST=E", "GENRE=Jazz", "GENRE=Blues")
				// Replace only the real file's Vorbis comment block, keeping valid audio.
				for offset := 4; offset+4 < len(data); {
					size := int(data[offset+1])<<16 | int(data[offset+2])<<8 | int(data[offset+3])
					if data[offset]&0x7f == 4 {
						updated := append([]byte{}, data[:offset]...)
						updated = append(updated, data[offset], byte(len(block)>>16), byte(len(block)>>8), byte(len(block)))
						updated = append(updated, block...)
						updated = append(updated, data[offset+4+size:]...)
						if err := os.WriteFile(path, updated, 0600); err != nil {
							t.Fatal(err)
						}
						break
					}
					offset += 4 + size
				}
				m, err = NewFFProber().Probe(context.Background(), path)
				if err != nil || !reflect.DeepEqual(m.Music.Artists, []string{"A; B", "C"}) || !reflect.DeepEqual(m.Music.Genres, []string{"Jazz", "Blues"}) || m.Music.AlbumArtist != "D, E" {
					t.Fatalf("real repeated comments %+v %v", m, err)
				}
			}
		})
	}
}
