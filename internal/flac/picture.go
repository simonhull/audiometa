package flac

import (
	"cmp"
	"encoding/binary"
	"errors"

	"github.com/simonhull/audiometa/internal/types"
)

// errShortPicture reports a PICTURE field that runs past the end of the block.
var errShortPicture = errors.New("field runs past the end of the PICTURE block")

// decodePicture decodes a PICTURE block payload: the picture type, the MIME
// type and the description (each a big-endian 32-bit length and its bytes),
// width, height, color depth and color count, then the picture data with its
// length. It reads nothing outside payload; Data aliases it. Bytes after the
// picture data are ignored.
func decodePicture(payload []byte) (types.Artwork, error) {
	p := picReader{b: payload}
	picType := p.u32()
	mime := p.field()
	desc := p.field()
	width, height := p.u32(), p.u32()
	p.take(8) // color depth and indexed colors, not used
	data := p.field()
	if p.err != nil {
		return types.Artwork{}, p.err
	}
	return types.Artwork{
		Data:        data,
		MIMEType:    string(mime),
		Type:        types.ArtworkTypeFromPictureType(picType),
		Description: string(desc),
		Width:       int(width),
		Height:      int(height),
	}, nil
}

// picReader takes big-endian fields off the front of b. After the first
// field that does not fit, err is set and every later read returns nothing.
type picReader struct {
	b   []byte
	err error
}

// take returns the next n bytes, or nil once a field has not fit.
func (p *picReader) take(n uint64) []byte {
	if p.err != nil || n > uint64(len(p.b)) {
		p.err = cmp.Or(p.err, errShortPicture)
		return nil
	}
	f := p.b[:n:n]
	p.b = p.b[n:]
	return f
}

// u32 returns the next big-endian 32-bit value, or 0 once a field has not fit.
func (p *picReader) u32() uint32 {
	if b := p.take(4); b != nil {
		return binary.BigEndian.Uint32(b)
	}
	return 0
}

// field returns the next length-prefixed byte string.
func (p *picReader) field() []byte {
	return p.take(uint64(p.u32()))
}
