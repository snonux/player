package model

import "sync"

// ScanProgress tracks the state of an in-progress or recently completed scan.
type ScanProgress struct {
	mu           sync.RWMutex
	Running      bool   `json:"running"`
	CurrentSet   string `json:"current_set,omitempty"`
	SetsTotal    int    `json:"sets_total"`
	SetsDone     int    `json:"sets_done"`
	FilesTotal   int    `json:"files_total"`
	FilesDone    int    `json:"files_done"`
	LastError    string `json:"last_error,omitempty"`
}

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

func (p *ScanProgress) SetCurrentSet(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.CurrentSet = name
}

func (p *ScanProgress) IncrementFile() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.FilesDone++
}

func (p *ScanProgress) SetFilesTotal(total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.FilesTotal = total
}

func (p *ScanProgress) IncrementSet() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.SetsDone++
}

func (p *ScanProgress) Done(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Running = false
	p.CurrentSet = ""
	if err != nil {
		p.LastError = err.Error()
	}
}

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
