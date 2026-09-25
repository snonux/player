package api

import (
	"net/url"
	"reflect"
	"testing"
)

// TestParseMediaListQuery_Filters checks the query parameters behind the
// combined favorites/search and file-size fixes reach the service filter,
// and that malformed values are ignored instead of filtering everything out.
func TestParseMediaListQuery_Filters(t *testing.T) {
	q, err := url.ParseQuery("search=10-chapter&favorites=true&set_id=3&filesize_min=100&filesize_max=2048&tags=a,+b+,a,,")
	if err != nil {
		t.Fatal(err)
	}
	filter := parseMediaListQuery(q)
	if filter.Search != "10-chapter" || !filter.Favorites || filter.SetID == nil || *filter.SetID != 3 {
		t.Fatalf("search/favorites/set not combined: %+v", filter)
	}
	if filter.MinFileSize == nil || *filter.MinFileSize != 100 || filter.MaxFileSize == nil || *filter.MaxFileSize != 2048 {
		t.Fatalf("file size bounds not parsed: min=%v max=%v", filter.MinFileSize, filter.MaxFileSize)
	}
	if !reflect.DeepEqual(filter.Tags, []string{"a", "b"}) {
		t.Fatalf("tags = %q, want trimmed unique non-empty [a b]", filter.Tags)
	}

	bad, _ := url.ParseQuery("filesize_min=big&filesize_max=1.5&favorites=yes&tags=,+,")
	filter = parseMediaListQuery(bad)
	if filter.MinFileSize != nil || filter.MaxFileSize != nil {
		t.Fatalf("invalid sizes must be ignored: min=%v max=%v", filter.MinFileSize, filter.MaxFileSize)
	}
	if filter.Favorites {
		t.Fatal("only true or 1 enables favorites")
	}
	if len(filter.Tags) != 0 {
		t.Fatalf("blank tag list must not filter, got %q", filter.Tags)
	}
}
