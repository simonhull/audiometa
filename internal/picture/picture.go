// Package picture encodes and decodes the FLAC PICTURE block (FLAC format section 8.8), which
// Ogg Vorbis and Opus also carry base64-encoded in METADATA_BLOCK_PICTURE comments.
package picture

import (
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/simonhull/audiometa/internal/types"
)

// ErrMalformed reports a PICTURE block whose fields run past its end.
var ErrMalformed = errors.New("malformed PICTURE block")

// fixedLen is the size of a block's fixed fields: the picture type, the two string lengths,
// width, height, color depth, color count and the data length.
const fixedLen = 32

// Decode decodes one PICTURE block: the picture type, the MIME type and the description
// (each a big-endian 32-bit length and its bytes), width, height, color depth and color
// count, then the picture data with its length. It reads nothing outside b; Data aliases b.
// Bytes after the picture data are ignored, and Width and Height above [math.MaxInt32] are
// clamped to it. A field that runs past the end of b gives an error wrapping [ErrMalformed].
func Decode(b []byte) (types.Artwork, error) {
	r := reader{b: b}
	picType := r.u32()
	mime := r.field()
	desc := r.field()
	width, height := r.u32(), r.u32()
	r.take(8) // color depth and indexed colors, not used
	data := r.field()
	if r.err != nil {
		return types.Artwork{}, r.err
	}
	return types.Artwork{
		Data:        data,
		MIMEType:    string(mime),
		Type:        types.ArtworkTypeFromPictureType(picType),
		Description: string(desc),
		Width:       int(min(width, math.MaxInt32)),
		Height:      int(min(height, math.MaxInt32)),
	}, nil
}

// reader takes big-endian fields off the front of b. After the first field that does not
// fit, err is set and every later read returns nothing.
type reader struct {
	b   []byte
	err error
}

// take returns the next n bytes, or nil once a field has not fit. The comparison is done in
// uint64 so a length near 4 GiB cannot wrap on 32-bit platforms.
func (r *reader) take(n uint64) []byte {
	if r.err != nil || n > uint64(len(r.b)) {
		r.err = cmp.Or(r.err, fmt.Errorf("%w: a field runs past the end of the block", ErrMalformed))
		return nil
	}
	f := r.b[:n:n]
	r.b = r.b[n:]
	return f
}

// u32 returns the next big-endian 32-bit value, or 0 once a field has not fit.
func (r *reader) u32() uint32 {
	if b := r.take(4); b != nil {
		return binary.BigEndian.Uint32(b)
	}
	return 0
}

// field returns the next length-prefixed byte string.
func (r *reader) field() []byte {
	return r.take(uint64(r.u32()))
}

// EncodedLen returns the size of a's encoded block, so callers can enforce a block size limit
// before [Encode] allocates.
func EncodedLen(a types.Artwork) int {
	return fixedLen + len(a.MIMEType) + len(a.Description) + len(a.Data)
}

// Encode validates a (a printable-ASCII MIME type, a picture type from Other to Publisher
// logotype, a UTF-8 description) and encodes it, with color depth and color count 0.
// Failures wrap [types.ErrUnsupportedWrite]. Encode does not limit the block's size; see
// [EncodedLen].
func Encode(a types.Artwork) ([]byte, error) {
	for _, c := range []byte(a.MIMEType) {
		if c < 0x20 || c > 0x7E {
			return nil, fmt.Errorf("%w: MIME type %q is not printable ASCII", types.ErrUnsupportedWrite, a.MIMEType)
		}
	}
	if a.Type < types.ArtworkOther || a.Type > types.ArtworkPublisherLogotype {
		return nil, fmt.Errorf("%w: picture type %d", types.ErrUnsupportedWrite, a.Type)
	}
	if !utf8.ValidString(a.Description) {
		return nil, fmt.Errorf("%w: artwork description is not valid UTF-8", types.ErrUnsupportedWrite)
	}
	out := make([]byte, 0, EncodedLen(a))
	out = binary.BigEndian.AppendUint32(out, uint32(a.Type))
	out = appendBytes(out, []byte(a.MIMEType))
	out = appendBytes(out, []byte(a.Description))
	out = binary.BigEndian.AppendUint32(out, dimension(a.Width))
	out = binary.BigEndian.AppendUint32(out, dimension(a.Height))
	out = binary.BigEndian.AppendUint32(out, 0) // color depth: unknown
	out = binary.BigEndian.AppendUint32(out, 0) // colors used: not indexed
	return appendBytes(out, a.Data), nil
}

// appendBytes appends b preceded by its big-endian 32-bit length. Callers bound len(b) by
// the block size limit first.
func appendBytes(out, b []byte) []byte {
	out = binary.BigEndian.AppendUint32(out, uint32(len(b))) //nolint:gosec // G115: callers bound len(b) by the block size limit
	return append(out, b...)
}

// dimension clamps an image dimension to the uint32 range (negative is 0).
func dimension(n int) uint32 {
	return uint32(min(max(int64(n), 0), math.MaxUint32))
}

// Type returns a block's picture type (its first four bytes, big-endian), or 0 (Other) when
// b is shorter.
func Type(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}
