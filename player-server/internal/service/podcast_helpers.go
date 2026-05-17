package service

import (
	"fmt"
	"strings"
)

const podcastSetName = "podcast"

func podcastFolderName(requestedName, title string, feedID int64) string {
	name := sanitizeSetName(requestedName)
	if name == "" {
		name = sanitizeSetName(title)
	}
	if name == "" {
		name = fmt.Sprintf("feed-%d", feedID)
	}
	return name
}

func sanitizeSetName(name string) string {
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "\\", "-")
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.TrimSpace(name)
	return name
}

func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "\\", "-")
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.TrimSpace(name)
	return name
}
