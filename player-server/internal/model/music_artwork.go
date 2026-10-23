package model

// MusicArtwork is a normalized image with a content-addressed cache revision.
type MusicArtwork struct {
	Data     []byte
	Revision string
}
