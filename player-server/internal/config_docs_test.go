package internal

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestConfigDocsListEveryVariable keeps the operator documentation in step
// with config.go: every environment variable it reads must have a row in
// the tables of AGENTS.md and docs/configuration.md.
func TestConfigDocsListEveryVariable(t *testing.T) {
	src, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, m := range regexp.MustCompile(`"([A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+|PORT)"`).FindAllStringSubmatch(string(src), -1) {
		names[m[1]] = true
	}
	if len(names) < 20 {
		t.Fatalf("found only %d variables in config.go; is the pattern still right?", len(names))
	}
	for _, doc := range []string{"../AGENTS.md", "../docs/configuration.md"} {
		data, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		for name := range names {
			if !strings.Contains(string(data), "| `"+name+"` |") {
				t.Errorf("%s has no table row for %s", doc, name)
			}
		}
	}
}
