package parsing

import (
	"testing"
)

func TestExtractSeriesPartFromText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Basic patterns
		{"Book with digit", "Book 2", "2"},
		{"Part with digit", "Part 3", "3"},
		{"Volume", "Volume 4", "4"},
		{"Vol abbreviated", "Vol. 5", "5"},
		{"Hash notation", "#6", "6"},
		{"Prefix with dash", "2 - North or Be Eaten", "2"},
		{"Prefix with colon", "3: The Monster in the Hollows", "3"},
		{"In parentheses", "The Warden (4)", "4"},
		{"Complex title", "The Wingfeather Saga, Book 2: North or Be Eaten", "2"},
		{"No series info", "The Martian", ""},
		{"Empty string", "", ""},
		{"Case insensitive book", "BOOK 7", "7"},
		{"Case insensitive part", "part 8", "8"},
		{"Volume full word", "Volume 9", "9"},
		{"With em dash", "10 — The Final Chapter", "10"},
		{"With en dash", "11 – Beginning", "11"},

		// Book 0 (prequels)
		{"Book 0", "Book 0", "0"},
		{"Book 0 with title", "Book 0: The Prequel", "0"},
		{"0 prefix", "0 - Before the Storm", "0"},
		{"Standalone 0", "0", "0"},
		{"Parentheses 0", "(0)", "0"},

		// Fractional books (novellas, half-books)
		{"Book 0.5", "Book 0.5", "0.5"},
		{"Book 1.5", "Book 1.5", "1.5"},
		{"Book 2.5", "Book 2.5", "2.5"},
		{"Part 0.5", "Part 0.5", "0.5"},
		{"Vol 3.5", "Vol. 3.5", "3.5"},
		{"Hash 0.5", "#0.5", "0.5"},
		{"Prefix 0.5 with dash", "0.5 - The Novella", "0.5"},
		{"Parentheses 1.5", "(1.5)", "1.5"},
		{"Standalone 0.5", "0.5", "0.5"},

		// Large series (Horus Heresy, Drizzt, etc.)
		{"Book 42", "Book 42", "42"},
		{"Book 54", "Book 54", "54"},
		{"Book 100", "Book 100", "100"},
		{"Book 150", "Book 150", "150"},
		{"Part 99", "Part 99", "99"},
		{"Vol 101", "Vol. 101", "101"},
		{"Hash 200", "#200", "200"},
		{"Prefix 42 with dash", "42 - The Answer", "42"},
		{"Three digit standalone", "150", "150"},

		// Leading zeros (should be normalized)
		{"Leading zero 01", "Book 01", "1"},
		{"Leading zero 001", "Book 001", "1"},
		{"Leading zero 09", "Book 09", "9"},
		{"Leading zero in prefix", "01 - First Book", "1"},
		{"Leading zero standalone", "01", "1"},

		// Fractional with leading zero (keep decimal)
		{"Fractional 01.5", "Book 01.5", "1.5"},
		{"Fractional 00.5", "Book 00.5", "0.5"},

		// Edge cases
		{"Multiple numbers", "Book 2 Chapter 5", "2"}, // First match wins
		{"Roman numerals ignored", "Book II", ""},     // Not supported
		{"Word numbers removed", "Book Two", ""},      // No longer supported
		{"Decimal without leading digit", ".5", ""},   // Invalid format
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractSeriesPartFromText(tt.input)
			if result != tt.expected {
				t.Errorf("ExtractSeriesPartFromText(%q) = %q, want %q",
					tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractSeriesPartFromPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			"Numeric prefix with dash",
			"/audiobooks/Author/Series/2 - North or Be Eaten/file.m4b",
			"2",
		},
		{
			"Book prefix with number",
			"/audiobooks/C.S. Lewis/Narnia/Book 2 - The Lion/file.m4b",
			"2",
		},
		{
			"Fractional position",
			"/audiobooks/Sapkowski/Witcher/0.5 - The Last Wish/file.m4b",
			"0.5",
		},
		{
			"Just number",
			"/audiobooks/Author/Series/3/file.m4b",
			"3",
		},
		{
			"No series position",
			"/audiobooks/Author/Single Book/The Martian.m4b",
			"",
		},
		{
			"Multiple numbers, first wins",
			"/audiobooks/Author/Series 1/2 - Book Two/file.m4b",
			"2",
		},
		{
			"Leading zero normalized",
			"/audiobooks/Author/Series/01 - First Book/file.m4b",
			"1",
		},
		{
			"Book keyword variant",
			"/audiobooks/Author/Series/Part 4 - Title/file.m4b",
			"4",
		},
		{
			"Empty path",
			"",
			"",
		},
		{
			"Root file",
			"/file.m4b",
			"",
		},
		{
			"Book 0 in path",
			"/audiobooks/Author/Series/Book 0 - Prequel/file.m4b",
			"0",
		},
		{
			"Large series number",
			"/audiobooks/Author/Horus Heresy/Book 54 - The Buried Dagger/file.m4b",
			"54",
		},
		{
			"Three digit book",
			"/audiobooks/Author/Series/150 - Final Book/file.m4b",
			"150",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractSeriesPartFromPath(tt.path)
			if result != tt.expected {
				t.Errorf("ExtractSeriesPartFromPath(%q) = %q, want %q",
					tt.path, result, tt.expected)
			}
		})
	}
}

func TestNormalizeSeriesPart(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Simple number", "5", "5"},
		{"Leading zero", "01", "1"},
		{"Multiple leading zeros", "001", "1"},
		{"Zero", "0", "0"},
		{"Large number", "150", "150"},
		{"Decimal", "0.5", "0.5"},
		{"Decimal with leading zero", "01.5", "1.5"},
		{"Decimal no leading digit", ".5", "0.5"}, // Normalize to proper format
		{"Empty string", "", ""},
		{"Non-numeric", "abc", "abc"}, // Fallback
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeSeriesPart(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeSeriesPart(%q) = %q, want %q",
					tt.input, result, tt.expected)
			}
		})
	}
}
