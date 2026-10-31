package types

import "testing"

func TestAppendWarning(t *testing.T) {
	tests := []struct {
		name     string
		existing int
		want     int
	}{
		{"empty", 0, 1},
		{"below cap", MaxWarnings - 1, MaxWarnings},
		{"at cap adds truncation note", MaxWarnings, MaxWarnings + 1},
		{"past cap drops", MaxWarnings + 1, MaxWarnings + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := make([]Warning, tt.existing)
			got := AppendWarning(ws, Warning{Stage: StageMetadata, Message: "x"})
			if len(got) != tt.want {
				t.Fatalf("len = %d, want %d", len(got), tt.want)
			}
		})
	}
}
