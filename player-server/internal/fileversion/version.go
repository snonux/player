// Package fileversion provides opaque, bounded file-change validators.
package fileversion

import (
	"crypto/sha256"
	"fmt"
	"os"
	"reflect"
	"strings"
)

// FromInfo identifies the file revision observed by Stat. Unix inode/device and
// change time detect replacement and writes that restore size/mtime. Access time
// is excluded so reading never invalidates a copy. The hash exposes no host
// paths, inode numbers or timestamps, and does not read media bytes. Empty means
// the platform did not expose inode/change time; callers must not claim a
// verified source validator in that case.
func FromInfo(info os.FileInfo) string {
	var stamp strings.Builder
	_, _ = fmt.Fprintf(&stamp, "%d:%d", info.Size(), info.ModTime().UnixNano())
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer && !value.IsNil() {
		value = value.Elem()
	}
	if value.Kind() == reflect.Struct {
		hasDevice, hasInode, hasChange := false, false, false
		for _, name := range []string{"Dev", "Ino", "Ctim", "Ctimespec", "Ctime", "Ctimensec", "CreationTime", "LastWriteTime"} {
			field := value.FieldByName(name)
			if field.IsValid() && field.CanInterface() {
				if name == "Dev" {
					hasDevice = true
				}
				if name == "Ino" {
					hasInode = true
				}
				if name == "Ctim" || name == "Ctimespec" || name == "Ctime" {
					hasChange = true
				}
				_, _ = fmt.Fprintf(&stamp, ":%s=%v", name, field.Interface())
			}
		}
		if !hasDevice || !hasInode || !hasChange {
			return ""
		}
	} else {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(stamp.String())))
}
