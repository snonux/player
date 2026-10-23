package probe

import (
	"math"
	"strconv"
	"strings"

	"github.com/snonux/player/internal/model"
)

func parseAudioTags(m *model.MusicMetadata, tags map[string]string) {
	for _, key := range []string{"bpm", "tbpm", "tempo"} {
		if tags[key] != "" {
			m.BPM = audioNumber(tags[key], false, true)
			break
		}
	}
	m.ReplayGainTrackGainDB = audioNumber(tags["replaygain_track_gain"], true, false)
	m.ReplayGainAlbumGainDB = audioNumber(tags["replaygain_album_gain"], true, false)
	m.ReplayGainTrackPeak = audioNumber(tags["replaygain_track_peak"], false, false)
	m.ReplayGainAlbumPeak = audioNumber(tags["replaygain_album_peak"], false, false)
}

func audioNumber(value string, gain, positive bool) *float64 {
	value = strings.TrimSpace(value)
	if gain && strings.HasSuffix(strings.ToLower(value), "db") {
		value = strings.TrimSpace(value[:len(value)-2])
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) || (!gain && number < 0) || (positive && number == 0) {
		return nil
	}
	return &number
}
