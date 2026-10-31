package types

import (
	"fmt"

	"github.com/simonhull/audiometa/internal/binary"
)

// OutOfBoundsError is returned when attempting to read beyond file bounds.
// It is defined in internal/binary, which cannot import this package.
type OutOfBoundsError = binary.OutOfBoundsError

// UnsupportedFormatError is returned when the file format is not M4B/M4A.
type UnsupportedFormatError struct {
	Path   string
	Reason string
}

func (e *UnsupportedFormatError) Error() string {
	return fmt.Sprintf("%s: unsupported format: %s", e.Path, e.Reason)
}

// CorruptedFileError is returned when file structure is invalid.
type CorruptedFileError struct {
	Path   string
	Reason string
	Offset int64
}

func (e *CorruptedFileError) Error() string {
	return fmt.Sprintf("%s: corrupted file at offset %d: %s", e.Path, e.Offset, e.Reason)
}

// Parsing stages reported in Warning.Stage.
const (
	StageMetadata  = "metadata"
	StageTechnical = "technical"
	StageChapters  = "chapters"
	StageArtwork   = "artwork"
)

// Warning represents a non-fatal issue encountered during parsing.
//
// Warnings indicate problems that don't prevent metadata extraction but
// may indicate corrupted or unusual data. Examples include:
//   - Missing optional fields
//   - Invalid encoding in a tag
//   - Corrupted artwork
//   - Unknown tag keys
//
// Warnings are collected in File.Warnings during parsing.
type Warning struct {
	// Stage where the warning occurred
	Stage string // one of StageMetadata, StageTechnical, StageChapters, StageArtwork

	// Warning message
	Message string

	// Err is the underlying error, if any. Callers can use errors.Is/As
	// against this via Warning.Unwrap(). May be nil for warnings that
	// don't originate from a Go error value.
	Err error

	// File offset where the issue occurred (0 if not applicable)
	Offset int64
}

// String returns a human-readable warning message.
func (w Warning) String() string {
	if w.Offset > 0 {
		return fmt.Sprintf("%s (at offset %d): %s", w.Stage, w.Offset, w.Message)
	}
	return fmt.Sprintf("%s: %s", w.Stage, w.Message)
}

// MaxWarnings is the most warnings a single file accumulates. Once reached, one
// final warning notes the truncation and further warnings are dropped, so a
// crafted file cannot grow File.Warnings without bound.
const MaxWarnings = 100

// AppendWarning appends w to ws unless the per-file cap has been reached.
func AppendWarning(ws []Warning, w Warning) []Warning {
	switch {
	case len(ws) < MaxWarnings:
		return append(ws, w)
	case len(ws) == MaxWarnings:
		return append(ws, Warning{
			Stage:   w.Stage,
			Message: fmt.Sprintf("further warnings suppressed after %d", MaxWarnings),
			Offset:  w.Offset,
		})
	default:
		return ws
	}
}

// Unwrap returns the underlying error so callers can use errors.Is/As.
func (w Warning) Unwrap() error { return w.Err }
