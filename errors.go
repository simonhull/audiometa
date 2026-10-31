package audiometa

import (
	"github.com/simonhull/audiometa/internal/types"
)

// OutOfBoundsError reports a read that would run past the end of the file,
// usually because the file is truncated or a length field is corrupt.
//
// Its fields give the file Path, the Offset and Length of the attempted read,
// the file Size, and What was being read. It is returned wrapped, so match it
// with [errors.As]. It can be the cause of an [Open] error, of a [Warning]
// (see Warning.Err) and therefore of a [WithStrictParsing] error, or of a
// [File.ExtractArtwork] error.
type OutOfBoundsError = types.OutOfBoundsError

// UnsupportedFormatError reports that a file is not in a format audiometa can
// parse: its contents match no known signature, its header cannot be read, or
// it is a recognized format without a parser (WAV, AIFF).
//
// Path names the file and Reason says why. [Open] and [DetectFormat] return
// it; match it with [errors.As].
type UnsupportedFormatError = types.UnsupportedFormatError

// CorruptedFileError reports that a file's container structure is invalid,
// for example bad FLAC or Ogg magic bytes or an MP4 atom that overruns its
// parent.
//
// Path names the file, Reason describes the problem and Offset is the file
// offset where it was found (0 if unknown). A panic inside a parser is also
// reported as a CorruptedFileError. Match it with [errors.As].
type CorruptedFileError = types.CorruptedFileError

// Warning describes a non-fatal problem found while parsing, such as an
// undecodable tag frame or an unreadable chapter table. Warnings are collected
// in File.Warnings; the data that could be parsed is still returned.
//
// Stage is one of [StageMetadata], [StageTechnical], [StageChapters] or
// [StageArtwork]; Message describes the problem; Offset is the file offset
// involved (0 if not applicable). Err, when non-nil, is the underlying error,
// and Warning's Unwrap method returns it for [errors.Is] and [errors.As].
//
// A file records at most 100 warnings plus one noting that further warnings
// were suppressed.
type Warning = types.Warning

// Parsing stages reported in Warning.Stage: tag parsing, technical audio
// properties, chapters, and artwork extraction.
const (
	StageMetadata  = types.StageMetadata
	StageTechnical = types.StageTechnical
	StageChapters  = types.StageChapters
	StageArtwork   = types.StageArtwork
)
