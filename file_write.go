package audiometa

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

// Save writes modified metadata back to the original file.
//
// This is an atomic operation: writes to a temporary file first, then renames
// to the original path. If any step fails, the original file remains unchanged.
//
// Options can be provided to customize save behavior:
//
//	err := file.Save(
//	    audiometa.WithBackup(".bak"),
//	    audiometa.WithValidation(),
//	)
//
// Returns UnsupportedWriteError if no writer is registered for the format.
func (f *File) Save(opts ...SaveOption) error {
	return f.SaveAs(f.Path, opts...)
}

// SaveAs writes the file to a new location.
//
// This is an atomic operation: writes to a temporary file first, then renames
// to the output path. If any step fails, any partially written data is cleaned up.
//
// Options can be provided to customize save behavior:
//
//	err := file.SaveAs("/new/path/song.m4a",
//	    audiometa.WithBackup(".bak"),
//	    audiometa.WithValidation(),
//	)
//
// Returns UnsupportedWriteError if no writer is registered for the format.
func (f *File) SaveAs(outputPath string, opts ...SaveOption) error { //nolint:gocyclo // Atomic file operations require sequential steps
	// Apply options
	options := defaultSaveOptions()
	for _, opt := range opts {
		opt(options)
	}

	// Get FormatWriter from registry
	writer := registry.GetWriter(f.Format)
	if writer == nil {
		return &types.UnsupportedWriteError{
			Format: f.Format,
			Reason: "no writer registered",
		}
	}

	// Ensure file is open (Reader_ must not be nil)
	if f.Reader_ == nil {
		return fmt.Errorf("file not open: reader is nil")
	}

	// Get original file's mod time if we need to preserve it
	var origModTime os.FileInfo
	if options.preserveModTime {
		info, err := os.Stat(f.Path)
		if err == nil {
			origModTime = info
		}
	}

	// Create temp file in same directory as output (for atomic rename)
	outputDir := filepath.Dir(outputPath)
	tempFile, err := os.CreateTemp(outputDir, ".audiometa-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// Ensure cleanup on any error
	success := false
	defer func() {
		if !success {
			_ = tempFile.Close()    //nolint:errcheck // Best effort cleanup
			_ = os.Remove(tempPath) //nolint:errcheck // Best effort cleanup
		}
	}()

	// Call writer.Write() with temp file
	if err := writer.Write(tempFile, &f.File, f.Reader_, f.Size); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	// Sync temp file (fsync) to ensure data is on disk
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}

	// Close temp file before rename
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Handle backup option (rename original to .bak before replace)
	if options.backupSuffix != "" {
		backupPath := outputPath + options.backupSuffix
		// Check if output file exists before trying to back it up
		if _, err := os.Stat(outputPath); err == nil {
			if err := os.Rename(outputPath, backupPath); err != nil {
				return fmt.Errorf("create backup: %w", err)
			}
		}
	}

	// Atomic rename temp -> output
	if err := os.Rename(tempPath, outputPath); err != nil {
		return fmt.Errorf("rename temp to output: %w", err)
	}

	// Mark success so defer doesn't clean up
	success = true

	// Handle preserveModTime option
	if options.preserveModTime && origModTime != nil {
		_ = os.Chtimes(outputPath, origModTime.ModTime(), origModTime.ModTime()) //nolint:errcheck // Non-fatal: file was written successfully
	}

	// Handle validate option (re-open and compare key fields)
	if options.validate {
		if err := f.validateWrittenFile(outputPath); err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}
	}

	return nil
}

// validateWrittenFile re-opens the file and compares key metadata fields.
func (f *File) validateWrittenFile(path string) error {
	// Re-open the written file
	written, err := Open(path)
	if err != nil {
		return fmt.Errorf("re-open: %w", err)
	}
	defer written.Close() //nolint:errcheck // Best effort close

	// Compare key fields
	if written.Tags.Title != f.Tags.Title {
		return fmt.Errorf("title mismatch: got %q, want %q", written.Tags.Title, f.Tags.Title)
	}
	if written.Tags.Artist != f.Tags.Artist {
		return fmt.Errorf("artist mismatch: got %q, want %q", written.Tags.Artist, f.Tags.Artist)
	}
	if written.Tags.Album != f.Tags.Album {
		return fmt.Errorf("album mismatch: got %q, want %q", written.Tags.Album, f.Tags.Album)
	}

	return nil
}

// FormatWriter is an alias to registry.FormatWriter for backwards compatibility.
// Re-exporting from internal/registry to maintain public API.
type FormatWriter = registry.FormatWriter

// RegisterWriter registers a writer for a format.
// This is called by format packages during initialization (init functions).
//
// This function is public to allow internal format packages to register themselves,
// but it's not intended for external use. Do not call this function.
func RegisterWriter(format types.Format, writer FormatWriter) {
	registry.RegisterWriter(format, writer)
}
