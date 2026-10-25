package internal

import "testing"

func TestScanAutomationConfig(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"SCAN_WATCH_SECONDS", "0", true}, {"SCAN_WATCH_SECONDS", "10", true},
		{"SCAN_WATCH_SECONDS", "9", false}, {"SCAN_WATCH_SECONDS", "86401", false},
		{"SCAN_INTERVAL_MINUTES", "0", true}, {"SCAN_INTERVAL_MINUTES", "1", true},
		{"SCAN_INTERVAL_MINUTES", "10081", false}, {"SCAN_INTERVAL_MINUTES", "-1", false},
	} {
		t.Run(tc.name+tc.value, func(t *testing.T) {
			t.Setenv("SCAN_WATCH_SECONDS", "0")
			t.Setenv("SCAN_INTERVAL_MINUTES", "0")
			t.Setenv(tc.name, tc.value)
			cfg, err := LoadConfig()
			if (err == nil) != tc.valid {
				t.Fatalf("config=%+v err=%v", cfg, err)
			}
		})
	}
}
