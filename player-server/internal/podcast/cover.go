package podcast

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// DownloadCoverImage fetches a podcast cover image and saves it to the set
// folder as cover.jpg, replacing the old cover only after a complete download.
func DownloadCoverImage(client *http.Client, imageURL, setPath string) error {
	if imageURL == "" {
		return nil
	}

	resp, err := client.Get(imageURL)
	if err != nil {
		return fmt.Errorf("fetch cover image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch cover image: status %d", resp.StatusCode)
	}

	// Write to a temporary file in the same folder and rename it into place,
	// so a failed or capped download never leaves a truncated cover.jpg in
	// place of the previous cover.
	coverPath := filepath.Join(setPath, "cover.jpg")
	f, err := os.CreateTemp(setPath, ".cover-*.jpg")
	if err != nil {
		return fmt.Errorf("create cover file: %w", err)
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath) // no-op once the rename succeeded

	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("write cover file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write cover file: %w", err)
	}
	if err := os.Rename(tmpPath, coverPath); err != nil {
		return fmt.Errorf("replace cover file: %w", err)
	}

	return nil
}
