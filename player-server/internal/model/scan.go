package model

import "sync"

// ScanProgress tracks the state of an in-progress or recently completed scan.
type ScanProgress struct {
	mu                sync.RWMutex
	Running           bool   `json:"running"`
	CurrentSet        string `json:"current_set,omitempty"`
	SetsTotal         int    `json:"sets_total"`
	SetsDone          int    `json:"sets_done"`
	FilesTotal        int    `json:"files_total"`
	FilesDone         int    `json:"files_done"`
	IdentityMoved     int    `json:"identity_moved,omitempty"`
	IdentityConflicts int    `json:"identity_conflicts,omitempty"`
	LastError         string `json:"last_error,omitempty"`
	State             string `json:"state,omitempty"`
	Trigger           string `json:"trigger,omitempty"`
	Pending           bool   `json:"pending"`
	WatchSeconds      int    `json:"watch_seconds"`
	IntervalMinutes   int    `json:"interval_minutes"`
	WatchError        string `json:"watch_error,omitempty"`
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
	p.IdentityMoved = 0
	p.IdentityConflicts = 0
	p.LastError = ""
}

// SetIdentityStats records how many songs were rematched and how many conflicts remain.
func (p *ScanProgress) SetIdentityStats(moved, conflicts int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.IdentityMoved = moved
	p.IdentityConflicts = conflicts
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

// AddFilesTotal adds to the total number of files discovered during the scan.
func (p *ScanProgress) AddFilesTotal(total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.FilesTotal += total
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
		Running:           p.Running,
		CurrentSet:        p.CurrentSet,
		SetsTotal:         p.SetsTotal,
		SetsDone:          p.SetsDone,
		FilesTotal:        p.FilesTotal,
		FilesDone:         p.FilesDone,
		IdentityMoved:     p.IdentityMoved,
		IdentityConflicts: p.IdentityConflicts,
		LastError:         p.LastError,
		State:             p.State,
		Trigger:           p.Trigger,
		Pending:           p.Pending,
		WatchSeconds:      p.WatchSeconds,
		IntervalMinutes:   p.IntervalMinutes,
		WatchError:        p.WatchError,
	}
}
