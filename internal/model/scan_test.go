package model

import (
	"errors"
	"sync"
	"testing"
)

func TestScanProgress_Start(t *testing.T) {
	var p ScanProgress
	p.Start(3)
	cp := p.Copy()
	if !cp.Running {
		t.Error("expected Running to be true")
	}
	if cp.SetsTotal != 3 {
		t.Errorf("SetsTotal = %d, want 3", cp.SetsTotal)
	}
	if cp.SetsDone != 0 {
		t.Errorf("SetsDone = %d, want 0", cp.SetsDone)
	}
	if cp.FilesTotal != 0 {
		t.Errorf("FilesTotal = %d, want 0", cp.FilesTotal)
	}
	if cp.FilesDone != 0 {
		t.Errorf("FilesDone = %d, want 0", cp.FilesDone)
	}
	if cp.LastError != "" {
		t.Errorf("LastError = %q, want empty", cp.LastError)
	}
}

func TestScanProgress_SetCurrentSet(t *testing.T) {
	var p ScanProgress
	p.Start(1)
	p.SetCurrentSet("movies")
	cp := p.Copy()
	if cp.CurrentSet != "movies" {
		t.Errorf("CurrentSet = %q, want movies", cp.CurrentSet)
	}
}

func TestScanProgress_SetFilesTotal(t *testing.T) {
	var p ScanProgress
	p.Start(1)
	p.SetFilesTotal(42)
	cp := p.Copy()
	if cp.FilesTotal != 42 {
		t.Errorf("FilesTotal = %d, want 42", cp.FilesTotal)
	}
}

func TestScanProgress_AddFilesTotal(t *testing.T) {
	var p ScanProgress
	p.Start(2)
	p.AddFilesTotal(10)
	p.AddFilesTotal(15)
	cp := p.Copy()
	if cp.FilesTotal != 25 {
		t.Errorf("FilesTotal = %d, want 25", cp.FilesTotal)
	}
}

func TestScanProgress_IncrementFile(t *testing.T) {
	var p ScanProgress
	p.Start(1)
	p.IncrementFile()
	cp := p.Copy()
	if cp.FilesDone != 1 {
		t.Errorf("FilesDone = %d, want 1", cp.FilesDone)
	}
}

func TestScanProgress_IncrementSet(t *testing.T) {
	var p ScanProgress
	p.Start(2)
	p.IncrementSet()
	cp := p.Copy()
	if cp.SetsDone != 1 {
		t.Errorf("SetsDone = %d, want 1", cp.SetsDone)
	}
}

func TestScanProgress_Done(t *testing.T) {
	var p ScanProgress
	p.Start(1)
	p.Done(nil)
	cp := p.Copy()
	if cp.Running {
		t.Error("expected Running to be false")
	}
	if cp.CurrentSet != "" {
		t.Errorf("CurrentSet = %q, want empty", cp.CurrentSet)
	}
	if cp.LastError != "" {
		t.Errorf("LastError = %q, want empty", cp.LastError)
	}
}

func TestScanProgress_Done_WithError(t *testing.T) {
	var p ScanProgress
	p.Start(1)
	p.Done(errors.New("scan failed"))
	cp := p.Copy()
	if cp.LastError != "scan failed" {
		t.Errorf("LastError = %q, want \"scan failed\"", cp.LastError)
	}
}

func TestScanProgress_Copy_Isolation(t *testing.T) {
	var p ScanProgress
	p.Start(1)
	cp1 := p.Copy()
	p.IncrementSet()
	cp2 := p.Copy()

	if cp1.SetsDone != 0 {
		t.Errorf("cp1.SetsDone = %d, want 0", cp1.SetsDone)
	}
	if cp2.SetsDone != 1 {
		t.Errorf("cp2.SetsDone = %d, want 1", cp2.SetsDone)
	}
}

func TestScanProgress_ConcurrentAccess(t *testing.T) {
	var p ScanProgress
	p.Start(2)
	p.SetFilesTotal(100)

	start := make(chan struct{})
	done := make(chan struct{})
	var copies sync.WaitGroup
	go func() {
		<-start
		for i := 0; i < 50; i++ {
			p.IncrementFile()
		}
		close(done)
	}()

	for i := 0; i < 50; i++ {
		copies.Add(1)
		go func() {
			defer copies.Done()
			<-start
			_ = p.Copy()
		}()
	}
	close(start)
	copies.Wait()
	<-done

	cp := p.Copy()
	if cp.FilesDone != 50 {
		t.Errorf("FilesDone = %d, want 50", cp.FilesDone)
	}
}
