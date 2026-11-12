// Package vorbis provides shared Vorbis comment parsing utilities.
//
// Vorbis comments are used by FLAC, Ogg Vorbis and Opus. The format is the
// same in all three: UTF-8 strings in "KEY=VALUE" form, with keys matched
// case-insensitively.
package vorbis

import (
	"fmt"
	"strings"

	"github.com/simonhull/audiometa/internal/types"
)

// ParseComment parses a single Vorbis comment in "KEY=VALUE" format and records it
// in the raw tags of file under its upper-cased name ("title=Foo" is retrievable
// as Get("TITLE")). It sets no typed field: those are derived from the raw tags
// by [types.DeriveFields], once the whole list is recorded ([Apply]).
//
// Field names are matched case-insensitively.
//
// Returns an error if the comment is not in valid "KEY=VALUE" format.
func ParseComment(comment string, file *types.File) error {
	key, value, ok := strings.Cut(comment, "=")
	if !ok {
		return fmt.Errorf("missing '=' in comment: %s", comment)
	}

	// Field names are case-insensitive per the Vorbis comment spec; canonicalize to upper case so
	// "title=" maps like "TITLE=" and the raw map has one key per logical tag.
	file.Tags.AddRaw(strings.ToUpper(key), value)
	return nil
}
