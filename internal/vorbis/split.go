package vorbis

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	abinary "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// Comments is the body of a Vorbis comment header (Vorbis I section 5.2.1): the vendor
// string, the user comments in file order, and any bytes after the last comment (the Ogg
// Vorbis framing bit, Opus padding), all aliasing the decoded payload.
type Comments struct {
	Vendor  []byte
	Entries [][]byte // raw "KEY=VALUE" bytes
	Tail    []byte
}

// ErrTruncated reports a length field that runs past the end of the comment data.
var ErrTruncated = errors.New("Vorbis comment length runs past the end of the data") //nolint:staticcheck // ST1005: Vorbis is a proper noun

// Split decodes payload: a little-endian 32-bit vendor length and the vendor string, a
// little-endian 32-bit comment count, then each comment as a little-endian 32-bit length
// and its bytes. It reads nothing outside payload and never sizes an allocation from the
// declared count alone.
//
// When a length runs past the end of payload, Split returns the comments decoded so far
// (the Tail is nil) with an error wrapping [ErrTruncated] that names the field ("vendor
// string", "comment count", "comment 3"). It checks ctx between comments and returns
// ctx.Err() once ctx is done.
func Split(ctx context.Context, payload []byte) (Comments, error) {
	var c Comments
	vendor, rest, ok := takeLE(payload)
	if !ok {
		return c, fmt.Errorf("vendor string: %w", ErrTruncated)
	}
	c.Vendor = vendor
	if len(rest) < 4 {
		return c, fmt.Errorf("comment count: %w", ErrTruncated)
	}
	count := binary.LittleEndian.Uint32(rest)
	rest = rest[4:]
	// Each comment takes at least 4 bytes, so no more than len(rest)/4 fit.
	n := len(rest) / 4
	if uint64(count) < uint64(n) {
		n = int(count)
	}
	c.Entries = make([][]byte, 0, n)
	for i := range count {
		if err := abinary.Canceled(ctx, len(c.Entries)); err != nil {
			return c, err
		}
		var entry []byte
		if entry, rest, ok = takeLE(rest); !ok {
			return c, fmt.Errorf("comment %d: %w", i, ErrTruncated)
		}
		c.Entries = append(c.Entries, entry)
	}
	c.Tail = rest
	return c, nil
}

// takeLE splits a little-endian 32-bit length and that many bytes off b. The comparison is
// done in int64 so a length near 4 GiB cannot wrap on 32-bit platforms.
func takeLE(b []byte) (field, rest []byte, ok bool) {
	if len(b) < 4 {
		return nil, nil, false
	}
	n := int64(binary.LittleEndian.Uint32(b))
	if n > int64(len(b)-4) {
		return nil, nil, false
	}
	return b[4 : 4+n], b[4+n:], true
}

// Encode returns the payload [Split] decodes into c. Lengths beyond the 32-bit range are
// not checked: such a payload exceeds every container's size limit.
func (c Comments) Encode() []byte {
	n := 8 + len(c.Vendor) + len(c.Tail)
	for _, e := range c.Entries {
		n += 4 + len(e)
	}
	out := make([]byte, 0, n)
	out = appendLE(out, c.Vendor)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(c.Entries))) //nolint:gosec // G115: bounded by the container size limit
	for _, e := range c.Entries {
		out = appendLE(out, e)
	}
	return append(out, c.Tail...)
}

// appendLE appends b preceded by its little-endian 32-bit length.
func appendLE(out, b []byte) []byte {
	out = binary.LittleEndian.AppendUint32(out, uint32(len(b))) //nolint:gosec // G115: bounded by the container size limit
	return append(out, b...)
}

// Apply records each entry on file with [ParseComment], adding one StageMetadata warning
// ("invalid Vorbis comment: ...", Err set) for each entry ParseComment rejects, then derives
// the typed fields of format from the raw tags ([types.DeriveFields]). It returns the entries
// as strings for [ParseChapters]. Each entry takes one value from the file's tag-value budget
// ([types.TakeValue]); the rest are ignored once it is spent.
func Apply(entries [][]byte, file *types.File, format types.Format) []string {
	comments := make([]string, 0, len(entries))
	for _, e := range entries {
		if !types.TakeValue(file) {
			break
		}
		comment := string(e)
		comments = append(comments, comment)
		if err := ParseComment(comment, file); err != nil {
			// Non-fatal: add a warning and continue.
			types.Warn(file, types.StageMetadata, 0, err, fmt.Sprintf("invalid Vorbis comment: %s", err))
		}
	}
	types.DeriveFields(&file.Tags, format)
	return comments
}
