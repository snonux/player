package model

import "sync"

// ScanProgress tracks the state of an in-progress or recently completed scan.
type ScanProgress struct {
	mu         sync.RWMutex
	Running    bool   `json:"running"`
	CurrentSet string `json:"current_set,omitempty"`
	SetsTotal  int    `json:"sets_total"`
	SetsDone   int    `json:"sets_done"`
	FilesTotal int    `json:"files_total"`
	FilesDone  int    `json:"files_done"`
	LastError  string `json:"last_error,omitempty"`
}

// Start marks a scan running and resets counters for the given set count.
func (p *ScanProgress) Start(setsTotal int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Running = true
	p.SetsTotal = setsTotal
	p.SetsDone = 0
	p.FilesTotal = 0
	p.FilesDone = 0
	p.LastError = ""
}

// SetCurrentSet records the set currently being scanned.
func (p *ScanProgress) SetCurrentSet(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.CurrentSet = name
}

// IncrementFile increments the completed file count.
func (p *ScanProgress) IncrementFile() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.FilesDone++
}

// SetFilesTotal records the number of files to scan.
func (p *ScanProgress) SetFilesTotal(total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.FilesTotal = total
}

// IncrementSet increments the completed set count.
func (p *ScanProgress) IncrementSet() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.SetsDone++
}

// Done marks the scan complete and records an error message when provided.
func (p *ScanProgress) Done(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Running = false
	p.CurrentSet = ""
	if err != nil {
		p.LastError = err.Error()
	}
}

// Copy returns a race-safe snapshot of the scan progress.
func (p *ScanProgress) Copy() ScanProgress {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return ScanProgress{
		Running:    p.Running,
		CurrentSet: p.CurrentSet,
		SetsTotal:  p.SetsTotal,
		SetsDone:   p.SetsDone,
		FilesTotal: p.FilesTotal,
		FilesDone:  p.FilesDone,
		LastError:  p.LastError,
	}
}
