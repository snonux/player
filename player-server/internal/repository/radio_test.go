package repository

import (
	"github.com/snonux/player/internal/model"
	"context"
	"path/filepath"
	"testing"
)

func TestRadioStationPersistenceAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "radio.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	station := model.RadioStation{Name: "Local", StreamURL: "http://127.0.0.1/live", HomepageURL: "https://example.com", ArtworkURL: "http://127.0.0.1/art", LocalNetwork: true}
	if err := store.SaveRadioStation(ctx, &station); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	items, err := store.ListRadioStations(ctx)
	if err != nil || len(items) != 1 || items[0] != station {
		t.Fatalf("restart %+v %v", items, err)
	}
	station.Name = "Edited"
	if err := store.SaveRadioStation(ctx, &station); err != nil {
		t.Fatal(err)
	}
	item, err := store.GetRadioStation(ctx, station.ID)
	if err != nil || item == nil || item.Name != "Edited" {
		t.Fatalf("edit %+v %v", item, err)
	}
	removed, err := store.DeleteRadioStation(ctx, station.ID)
	if err != nil || !removed {
		t.Fatalf("delete %v %v", removed, err)
	}
	item, err = store.GetRadioStation(ctx, station.ID)
	if err != nil || item != nil {
		t.Fatalf("removed %+v %v", item, err)
	}
}
