package probe

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

const maxMusicTagBytes = 16 << 20

// structuredMusicTags supplements ffprobe, whose flattened Vorbis dictionary
// cannot distinguish repeated comments from a literal semicolon in one name.
// Only actual comment boundaries become values; arbitrary audio bytes are
// never searched for apparent tags. Reads/allocations are bounded.
func structuredMusicTags(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var header [4]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return nil, err
	}
	switch string(header[:]) {
	case "fLaC":
		return flacComments(f)
	case "OggS":
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		return oggComments(io.LimitReader(f, maxMusicTagBytes))
	}
	return nil, nil
}

func flacComments(r io.Reader) (map[string]string, error) {
	consumed := 0
	for consumed < maxMusicTagBytes {
		var header [4]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return nil, err
		}
		size := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
		consumed += size + 4
		if consumed > maxMusicTagBytes {
			return nil, fmt.Errorf("music tags exceed limit")
		}
		if header[0]&0x7f == 4 {
			block := make([]byte, size)
			if _, err := io.ReadFull(r, block); err != nil {
				return nil, err
			}
			return vorbisComments(block)
		}
		if _, err := io.CopyN(io.Discard, r, int64(size)); err != nil {
			return nil, err
		}
		if header[0]&0x80 != 0 {
			return nil, nil
		}
	}
	return nil, fmt.Errorf("music tags exceed limit")
}

func oggComments(r io.Reader) (map[string]string, error) {
	var packet []byte
	var serial uint32
	for page := 0; page < 4096; page++ {
		var header [27]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return nil, err
		}
		if string(header[:4]) != "OggS" || header[4] != 0 {
			return nil, fmt.Errorf("invalid Ogg page")
		}
		current := binary.LittleEndian.Uint32(header[14:18])
		if page == 0 {
			serial = current
		}
		if current != serial {
			return nil, nil
		} // don't mix multiplexed streams
		laces := make([]byte, int(header[26]))
		if _, err := io.ReadFull(r, laces); err != nil {
			return nil, err
		}
		for _, size := range laces {
			if len(packet)+int(size) > maxMusicTagBytes {
				return nil, fmt.Errorf("music packet exceeds limit")
			}
			part := make([]byte, int(size))
			if _, err := io.ReadFull(r, part); err != nil {
				return nil, err
			}
			packet = append(packet, part...)
			if size == 255 {
				continue
			}
			switch {
			case bytes.HasPrefix(packet, []byte("\x03vorbis")):
				return vorbisComments(packet[7:])
			case bytes.HasPrefix(packet, []byte("OpusTags")):
				return vorbisComments(packet[8:])
			case bytes.HasPrefix(packet, []byte("\x01vorbis")), bytes.HasPrefix(packet, []byte("OpusHead")):
				packet = packet[:0]
			default:
				return nil, nil
			}
		}
	}
	return nil, fmt.Errorf("too many Ogg tag pages")
}

func vorbisComments(data []byte) (map[string]string, error) {
	r := bytes.NewReader(data)
	readSize := func() (uint32, error) { var n uint32; err := binary.Read(r, binary.LittleEndian, &n); return n, err }
	size, err := readSize()
	if err != nil || uint64(size) > uint64(r.Len()) {
		return nil, fmt.Errorf("invalid Vorbis vendor size")
	}
	if _, err := r.Seek(int64(size), io.SeekCurrent); err != nil {
		return nil, err
	}
	count, err := readSize()
	if err != nil || uint64(count)*4 > uint64(r.Len()) {
		return nil, fmt.Errorf("invalid Vorbis comment count")
	}
	values := map[string][]string{}
	for i := uint32(0); i < count; i++ {
		size, err := readSize()
		if err != nil || uint64(size) > uint64(r.Len()) {
			return nil, fmt.Errorf("invalid Vorbis comment size")
		}
		entry := make([]byte, int(size))
		if _, err := io.ReadFull(r, entry); err != nil {
			return nil, err
		}
		key, value, ok := strings.Cut(string(entry), "=")
		if !ok {
			continue
		}
		key = strings.ToLower(key)
		switch key {
		case "albumartist", "album artist":
			key = "album_artist"
		case "albumartists":
			key = "album_artists"
		}
		switch key {
		case "artist", "artists", "album_artist", "albumartist", "album_artists", "albumartists", "genre", "genres", "composer", "composers", "performer", "performers":
			values[key] = append(values[key], value)
		default:
			if structuredReleaseTag(key) {
				values[key] = append(values[key], value)
			}
		}
	}
	result := map[string]string{}
	for key, entries := range values {
		result[key] = strings.Join(entries, "\x00")
	}
	return result, nil
}
