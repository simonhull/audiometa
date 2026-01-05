package types

import "fmt"

// OutOfBoundsError is returned when attempting to read beyond file bounds.
type OutOfBoundsError struct {
	Path   string
	What   string
	Offset int64
	Length int
	Size   int64
}

func (e *OutOfBoundsError) Error() string {
	if e.Offset >= e.Size {
		return fmt.Sprintf("%s: offset %d out of bounds (file size: %d) while reading %s",
			e.Path, e.Offset, e.Size, e.What)
	}
	return fmt.Sprintf("%s: read of %d bytes at offset %d would exceed file size %d while reading %s",
		e.Path, e.Length, e.Offset, e.Size, e.What)
}

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
	Stage string // "metadata", "technical", "chapters", "artwork"

	// Warning message
	Message string

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

// UnsupportedWriteError indicates write is not supported for this format.
type UnsupportedWriteError struct {
	Reason string
	Format Format
}

func (e *UnsupportedWriteError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("write not supported for %s: %s", e.Format, e.Reason)
	}
	return fmt.Sprintf("write not supported for %s", e.Format)
}
