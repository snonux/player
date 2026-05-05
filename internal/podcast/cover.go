package podcast

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// DownloadCoverImage fetches a podcast cover image and saves it to the set folder as cover.jpg.
func DownloadCoverImage(imageURL, setPath string) error {
	if imageURL == "" {
		return nil
	}

	resp, err := http.Get(imageURL)
	if err != nil {
		return fmt.Errorf("fetch cover image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch cover image: status %d", resp.StatusCode)
	}

	coverPath := filepath.Join(setPath, "cover.jpg")
	f, err := os.Create(coverPath)
	if err != nil {
		return fmt.Errorf("create cover file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write cover file: %w", err)
	}

	return nil
}
