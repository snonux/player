package model

// PlaylistRules defines a bounded, dynamically evaluated music selection.
type PlaylistRules struct {
	Match      string              `json:"match"`
	Conditions []PlaylistCondition `json:"conditions"`
	Sort       string              `json:"sort"`
	Descending bool                `json:"descending"`
	Limit      int                 `json:"limit"`
}

// PlaylistCondition compares one song field with a validated scalar string.
type PlaylistCondition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}
