package probe

import "testing"

func TestEmbeddedArtworkSelection(t *testing.T) {
	for _, tc := range []struct {
		name, streams string
		want          int
	}{
		{"no streams", `[]`, -1},
		{"ordinary video is not art", `[{"index":0,"codec_type":"video"}]`, -1},
		{"audio disposition is not a picture", `[{"index":0,"codec_type":"audio","disposition":{"attached_pic":1}}]`, -1},
		{"first attached picture fallback", `[{"index":2,"codec_type":"video","disposition":{"attached_pic":1}},{"index":3,"codec_type":"video","disposition":{"attached_pic":1}}]`, 2},
		{"front replaces back", `[{"index":1,"codec_type":"video","disposition":{"attached_pic":1},"tags":{"comment":"Cover (back)"}},{"index":2,"codec_type":"video","disposition":{"attached_pic":1},"tags":{"comment":"Cover (FRONT)"}}]`, 2},
		{"back does not replace front", `[{"index":1,"codec_type":"video","disposition":{"attached_pic":1},"tags":{"comment":"Cover (front)"}},{"index":2,"codec_type":"video","disposition":{"attached_pic":1}}]`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			meta, err := parseFFprobeOutput([]byte(`{"streams":`+tc.streams+`}`), true)
			if err != nil {
				t.Fatal(err)
			}
			got := -1
			if meta.EmbeddedArtIndex != nil {
				got = *meta.EmbeddedArtIndex
			}
			if got != tc.want {
				t.Fatalf("picture index=%d, want %d", got, tc.want)
			}
		})
	}
}
