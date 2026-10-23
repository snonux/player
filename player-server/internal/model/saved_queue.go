package model

// SavedQueue is an account's portable Music session. Occurrence IDs distinguish
// repeated songs. No sources, credentials, playing state or device ownership travel.
type SavedQueue struct {
	Revision   int64             `json:"revision"`
	MutationID string            `json:"mutation_id"`
	Entries    []QueueOccurrence `json:"entries"`
	CurrentID  int64             `json:"current_id"`
	Position   float64           `json:"position_seconds"`
	Label      string            `json:"label"`
	Repeat     string            `json:"repeat"`
	Shuffle    bool              `json:"shuffle"`
	Canonical  []int64           `json:"canonical"`
	UpNext     []int64           `json:"up_next"`
}

// QueueOccurrence is an ordered occurrence of a library song.
type QueueOccurrence struct {
	ID      int64 `json:"id"`
	MediaID int64 `json:"media_id"`
}
