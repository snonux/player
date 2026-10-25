package fileversion

import (
	"os"
	"testing"
	"time"
)

type infoFixture struct{ sys any }

func (i infoFixture) Name() string       { return "fixture" }
func (i infoFixture) Size() int64        { return 10 }
func (i infoFixture) Mode() os.FileMode  { return 0600 }
func (i infoFixture) ModTime() time.Time { return time.Unix(1000, 0) }
func (i infoFixture) IsDir() bool        { return false }
func (i infoFixture) Sys() any           { return i.sys }

func TestRevisionChangeTimeAndAtime(t *testing.T) {
	type stat struct{ Dev, Ino, Ctim, Atim int64 }
	first := FromInfo(infoFixture{&stat{1, 2, 3, 4}})
	if first == "" {
		t.Fatal("supported checkpoint unavailable")
	}
	if first != FromInfo(infoFixture{&stat{1, 2, 3, 5}}) {
		t.Fatal("atime changed revision")
	}
	for _, next := range []stat{{1, 9, 3, 4}, {1, 2, 9, 4}, {9, 2, 3, 4}} {
		if first == FromInfo(infoFixture{&next}) {
			t.Fatal("replacement/write checkpoint reused revision")
		}
	}
	for _, unknown := range []any{nil, struct{ Mtime int64 }{10}, struct{ Ino int64 }{2}} {
		if FromInfo(infoFixture{unknown}) != "" {
			t.Fatal("unsupported platform claimed verified revision")
		}
	}
}
