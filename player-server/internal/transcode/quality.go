package transcode

// AudioProfile is one server-supported, bounded music rendition.
type AudioProfile struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Codec   string `json:"codec"`
	Bitrate int    `json:"bitrate_kbps"`
}

// AudioProfiles returns the server's selectable music quality profiles.
func AudioProfiles() []AudioProfile {
	return []AudioProfile{{"aac128", "Balanced · AAC 128 kbps", "aac", 128}, {"aac256", "High · AAC 256 kbps", "aac", 256}, {"mp3192", "Compatible · MP3 192 kbps", "mp3", 192}, {"opus96", "Data saver · Opus 96 kbps", "opus", 96}}
}

// AudioProfileKind resolves only server-supported profiles.
func AudioProfileKind(id string) (Kind, bool) {
	for i, p := range AudioProfiles() {
		if p.ID == id {
			return Kind(int(KindAAC128) + i), true
		}
	}
	return 0, false
}

func (k Kind) selectedAudio() bool { return k >= KindAAC128 && k <= KindOpus96 }
