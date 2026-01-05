package audiometa

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestFile_Save_UnsupportedFormat(t *testing.T) {
	// Create a file with no registered writer (e.g., FormatWAV)
	f := &File{
		File: types.File{
			Path:    "/tmp/test.wav",
			Format:  types.FormatWAV,
			Size:    1000,
			Reader_: nil, // Will be caught before writer check
		},
	}

	// Need a non-nil reader for the test to reach the writer check
	// Create a minimal bytes.Reader to satisfy the nil check
	f.Reader_ = &minimalReaderAt{}

	err := f.Save()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var unsupportedErr *types.UnsupportedWriteError
	if !errors.As(err, &unsupportedErr) {
		t.Fatalf("expected *UnsupportedWriteError, got %T: %v", err, err)
	}

	if unsupportedErr.Format != types.FormatWAV {
		t.Errorf("expected format WAV, got %v", unsupportedErr.Format)
	}

	if unsupportedErr.Reason != "no writer registered" {
		t.Errorf("expected reason 'no writer registered', got %q", unsupportedErr.Reason)
	}
}

func TestFile_SaveAs_UnsupportedFormat(t *testing.T) {
	// Create a temp directory for output
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "output.aiff")

	// Create a file with no registered writer (FormatAIFF has no writer)
	f := &File{
		File: types.File{
			Path:    "/tmp/test.aiff",
			Format:  types.FormatAIFF,
			Size:    1000,
			Reader_: &minimalReaderAt{},
		},
	}

	err := f.SaveAs(outputPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var unsupportedErr *types.UnsupportedWriteError
	if !errors.As(err, &unsupportedErr) {
		t.Fatalf("expected *UnsupportedWriteError, got %T: %v", err, err)
	}

	if unsupportedErr.Format != types.FormatAIFF {
		t.Errorf("expected format AIFF, got %v", unsupportedErr.Format)
	}
}

func TestFile_Save_NilReader(t *testing.T) {
	// Create a file with nil Reader_ to test that error path
	f := &File{
		File: types.File{
			Path:    "/tmp/test.wav",
			Format:  types.FormatWAV,
			Size:    1000,
			Reader_: nil,
		},
	}

	err := f.Save()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Should get UnsupportedWriteError before hitting nil reader check
	// because there's no writer for WAV
	var unsupportedErr *types.UnsupportedWriteError
	if !errors.As(err, &unsupportedErr) {
		t.Fatalf("expected *UnsupportedWriteError, got %T: %v", err, err)
	}
}

func TestUnsupportedWriteError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *types.UnsupportedWriteError
		expected string
	}{
		{
			name: "with reason",
			err: &types.UnsupportedWriteError{
				Format: types.FormatWAV,
				Reason: "no writer registered",
			},
			expected: "write not supported for WAV: no writer registered",
		},
		{
			name: "without reason",
			err: &types.UnsupportedWriteError{
				Format: types.FormatAIFF,
			},
			expected: "write not supported for AIFF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.expected {
				t.Errorf("Error() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestFile_SaveAs_Options(t *testing.T) {
	// Test that options are applied (even though write will fail)
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "output.wav")

	f := &File{
		File: types.File{
			Path:    "/tmp/test.wav",
			Format:  types.FormatWAV,
			Size:    1000,
			Reader_: &minimalReaderAt{},
		},
	}

	// Test with all options - should still fail with UnsupportedWriteError
	err := f.SaveAs(outputPath,
		WithBackup(".bak"),
		WithValidation(),
		WithPreserveModTime(),
	)

	var unsupportedErr *types.UnsupportedWriteError
	if !errors.As(err, &unsupportedErr) {
		t.Fatalf("expected *UnsupportedWriteError, got %T: %v", err, err)
	}
}

// minimalReaderAt is a minimal io.ReaderAt implementation for testing.
type minimalReaderAt struct{}

func (r *minimalReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	return 0, os.ErrNotExist
}
