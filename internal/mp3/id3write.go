package mp3

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// ID3v2 size limits for the writer.
const (
	maxTagBody      = 0x0FFFFFFF // largest synchsafe tag size
	maxV24FrameSize = 0x0FFFFFFF // largest synchsafe frame size
	maxV23FrameSize = 0xFFFFFFFF // largest big-endian frame size

	minGrowPadding = 2048    // padding added when a tag outgrows its space
	maxGrowPadding = 1 << 20 // cap on that padding

	flagExtendedHeader = 0x40 // ID3v2.3/2.4 tag header: extended header present
)

// wframe is one frame of a tag being rewritten.
type wframe struct {
	id      string
	payload []byte // the frame body after its header, with unsynchronisation undone
	header  []byte // the original header bytes, re-emitted verbatim; nil to encode a fresh one
	flags   uint16 // frame flags in the output version's layout
	edited  bool   // written fresh by an edit
}

// wtag is the frame list of a tag being rewritten.
type wtag struct {
	frames     []wframe
	oldTotal   int64 // on-disk size of the source tag (header, body and footer); 0 when untagged
	inVersion  byte  // source ID3v2 major version; 0 when untagged
	outVersion byte  // 3 or 4
}

// v22ToV24 maps ID3v2.2 frame IDs to their ID3v2.4 equivalents. Frames not
// listed cannot be converted.
var v22ToV24 = pairsOf(`
	BUF RBUF  CNT PCNT  COM COMM  ETC ETCO  GEO GEOB  MCI MCDI  MLL MLLT
	POP POPM  REV RVRB  SLT SYLT  STC SYTC  UFI UFID  ULT USLT
	TAL TALB  TBP TBPM  TCM TCOM  TCO TCON  TCR TCOP  TDY TDLY  TEN TENC
	TFT TFLT  TKE TKEY  TLA TLAN  TLE TLEN  TMT TMED  TOA TOPE  TOF TOFN
	TOL TOLY  TOT TOAL  TP1 TPE1  TP2 TPE2  TP3 TPE3  TP4 TPE4  TPA TPOS
	TPB TPUB  TRC TSRC  TRK TRCK  TSS TSSE  TT1 TIT1  TT2 TIT2  TT3 TIT3
	TXT TEXT  TXX TXXX  TYE TDRC  TOR TDOR
	WAF WOAF  WAR WOAR  WAS WOAS  WCM WCOM  WCP WCOP  WPB WPUB  WXX WXXX
	TCP TCMP  TST TSOT  TSP TSOP  TSA TSOA  TS2 TSO2  TSC TSOC
	GP1 GRP1  MVN MVNM  MVI MVIN
	PIC APIC`)

// pairsOf builds a map from whitespace-separated key-value pairs.
func pairsOf(s string) map[string]string {
	fields := strings.Fields(s)
	m := make(map[string]string, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		m[fields[i]] = fields[i+1]
	}
	return m
}

// readWTag reads the ID3v2 tag at the start of sr into a frame list. A file
// without an ID3v2 tag yields an empty list with output version 2.4.
func readWTag(ctx context.Context, sr *binutil.SafeReader, size int64) (*wtag, error) {
	if !hasID3Magic(sr, size) {
		return &wtag{outVersion: 4}, nil
	}
	if size < 10 {
		return nil, corrupted(sr, 0, "truncated ID3v2 header")
	}
	header, err := parseID3v2Header(sr)
	switch {
	case err != nil:
		return nil, fmt.Errorf("%w: ID3v2.%d tags cannot be written: %w", types.ErrUnsupportedWrite, header.Version, err)
	case header.Version == 2 && header.Flags&flagV22Compression != 0:
		return nil, fmt.Errorf("%w: compressed ID3v2.2 tag", types.ErrUnsupportedWrite)
	}
	t := &wtag{inVersion: header.Version, outVersion: header.Version, oldTotal: header.totalSize()}
	if header.Version == 2 {
		t.outVersion = 4
	}
	if t.oldTotal > size {
		return nil, corrupted(sr, 0, fmt.Sprintf("ID3v2 tag of %d bytes runs past the end of the file", t.oldTotal))
	}

	if err := sr.CheckLen(10, int(header.Size), 1, "ID3 tag"); err != nil {
		return nil, err
	}
	body := make([]byte, header.Size)
	if err := sr.ReadAt(body, 10, "ID3 tag"); err != nil {
		return nil, err
	}
	if header.Version != 4 && header.Flags&flagUnsync != 0 {
		body = deunsync(body)
	}
	start, err := extendedHeaderLen(sr, header, body)
	if err != nil {
		return nil, err
	}
	if err := t.walk(ctx, sr, header, body, start); err != nil {
		return nil, err
	}
	return t, nil
}

// hasID3Magic reports whether the file starts with "ID3".
func hasID3Magic(sr *binutil.SafeReader, size int64) bool {
	if size < 3 {
		return false
	}
	var magic [3]byte
	return sr.ReadAt(magic[:], 0, "ID3 magic") == nil && string(magic[:]) == "ID3"
}

// extendedHeaderLen returns the length of the extended header at the start of
// body, or 0 when there is none.
func extendedHeaderLen(sr *binutil.SafeReader, header ID3v2Header, body []byte) (int, error) {
	if header.Version == 2 || header.Flags&flagExtendedHeader == 0 {
		return 0, nil
	}
	if len(body) < 4 {
		return 0, corrupted(sr, 10, "truncated extended header")
	}
	var n uint64
	if header.Version == 4 {
		n = uint64(decodeSynchsafe(body[:4]))
	} else {
		n = uint64(binary.BigEndian.Uint32(body[:4])) + 4
	}
	if n > uint64(len(body)) {
		return 0, corrupted(sr, 10, fmt.Sprintf("extended header of %d bytes overruns the tag", n))
	}
	return int(n), nil //nolint:gosec // G115: n <= len(body), so it fits in int
}

// walk splits body (from offset start) into frames, stopping at padding.
func (t *wtag) walk(ctx context.Context, sr *binutil.SafeReader, header ID3v2Header, body []byte, start int) error {
	headerLen, idLen := 10, 4
	if header.Version == 2 {
		headerLen, idLen = 6, 3
	}
	for i, off := 0, start; off < len(body) && body[off] != 0; i++ {
		if err := binutil.Canceled(ctx, i); err != nil {
			return err
		}
		if len(body)-off < headerLen {
			return corrupted(sr, int64(10+off), "frame header overruns the ID3v2 tag")
		}
		hdr := body[off : off+headerLen]
		size := frameSizeFromHeader(header.Version, hdr)
		if size > uint64(len(body)-off-headerLen) { //nolint:gosec // G115: the check above guarantees len(body)-off >= headerLen
			return corrupted(sr, int64(10+off), fmt.Sprintf("frame %q of %d bytes overruns the ID3v2 tag", hdr[:idLen], size))
		}
		payload := body[off+headerLen : off+headerLen+int(size)] //nolint:gosec // G115: size <= len(body)
		f, err := t.newFrame(header, hdr, payload)
		if err != nil {
			return err
		}
		t.frames = append(t.frames, f)
		off += headerLen + int(size) //nolint:gosec // G115: size <= len(body)
	}
	return nil
}

// frameSizeFromHeader decodes the payload size from a frame header.
func frameSizeFromHeader(version byte, hdr []byte) uint64 {
	if version == 2 {
		return uint64(hdr[3])<<16 | uint64(hdr[4])<<8 | uint64(hdr[5])
	}
	return uint64(decodeFrameSize(version, hdr[4:8]))
}

// newFrame builds a wframe from a frame's on-disk header and payload,
// converting ID3v2.2 frames to ID3v2.4 and undoing ID3v2.4 unsynchronisation.
func (t *wtag) newFrame(header ID3v2Header, hdr, payload []byte) (wframe, error) {
	if header.Version == 2 {
		return convertV22Frame(string(hdr[:3]), payload)
	}
	f := wframe{
		id:      string(hdr[:4]),
		flags:   binary.BigEndian.Uint16(hdr[8:10]),
		payload: payload,
		header:  hdr,
	}
	if header.Version == 4 && (f.flags&v24FrameUnsync != 0 || header.Flags&flagUnsync != 0) {
		f.payload = deunsync(payload)
		f.flags &^= v24FrameUnsync
		f.header = nil
	}
	return f, nil
}

// convertV22Frame converts one ID3v2.2 frame to ID3v2.4.
func convertV22Frame(id string, payload []byte) (wframe, error) {
	if id == "CRA" || id == "CRM" {
		return wframe{}, fmt.Errorf("%w: ID3v2.2 %s frame", types.ErrDRMProtected, id)
	}
	newID, ok := v22ToV24[id]
	if !ok {
		return wframe{}, fmt.Errorf("%w: ID3v2.2 frame %q has no ID3v2.4 equivalent", types.ErrUnsupportedWrite, id)
	}
	if newID == frameIDAPIC {
		payload = convertPIC(payload)
	}
	return wframe{id: newID, payload: payload}, nil
}

// convertPIC turns an ID3v2.2 PIC body ([enc][format(3)][type][desc][data])
// into an APIC body ([enc][MIME\0][type][desc][data]).
func convertPIC(p []byte) []byte {
	if len(p) < 4 {
		return p
	}
	var mime string
	switch format := string(p[1:4]); strings.ToUpper(format) {
	case "JPG":
		mime = "image/jpeg"
	case "PNG":
		mime = "image/png"
	default:
		mime = "image/" + strings.ToLower(strings.TrimRight(format, "\x00 "))
	}
	out := make([]byte, 0, len(p)+len(mime)-2)
	out = append(out, p[0])
	out = append(out, mime...)
	out = append(out, 0)
	return append(out, p[4:]...)
}

// corrupted returns a CorruptedFileError for the file behind sr.
func corrupted(sr *binutil.SafeReader, off int64, reason string) error {
	return &types.CorruptedFileError{Path: sr.Path(), Offset: off, Reason: reason}
}

// serialize encodes the tag: the header (flags 0, no extended header, no
// footer), the frames and the padding. When the frames fit in the source
// tag's space the tag keeps its exact size; otherwise it gets fresh padding.
// An untagged source with no frames to write yields no tag at all.
func (t *wtag) serialize() ([]byte, error) {
	var frames []byte
	for _, f := range t.frames {
		var err error
		if frames, err = t.appendFrame(frames, f); err != nil {
			return nil, err
		}
	}
	n := len(frames)
	if t.oldTotal == 0 && n == 0 {
		return nil, nil
	}
	padding := t.padding(n)
	if err := checkTagSize(n + padding); err != nil {
		return nil, err
	}
	out := make([]byte, 0, 10+n+padding)
	out = append(out, 'I', 'D', '3', t.outVersion, 0, 0)
	out = appendSynchsafe(out, uint32(n+padding)) //nolint:gosec // G115: checkTagSize bounds n+padding by maxTagBody
	out = append(out, frames...)
	return append(out, make([]byte, padding)...), nil
}

// padding returns the padding after n bytes of frames.
func (t *wtag) padding(n int) int {
	if capacity := int(max(t.oldTotal-10, 0)); n <= capacity {
		return capacity - n
	}
	p := min(max(n/10, minGrowPadding), maxGrowPadding)
	if room := maxTagBody - n; room >= 0 && p > room {
		p = room
	}
	return p
}

// appendFrame appends f's header and payload to b.
func (t *wtag) appendFrame(b []byte, f wframe) ([]byte, error) {
	if f.header != nil {
		b = append(b, f.header...)
		return append(b, f.payload...), nil
	}
	if err := checkFrameSize(len(f.payload), t.outVersion); err != nil {
		return nil, fmt.Errorf("%w: frame %s", err, f.id)
	}
	b = append(b, f.id...)
	if t.outVersion == 4 {
		b = appendSynchsafe(b, uint32(len(f.payload))) //nolint:gosec // G115: checkFrameSize bounds it
	} else {
		b = binary.BigEndian.AppendUint32(b, uint32(len(f.payload))) //nolint:gosec // G115: checkFrameSize bounds it
	}
	b = binary.BigEndian.AppendUint16(b, f.flags)
	return append(b, f.payload...), nil
}

// appendSynchsafe appends n (at most 0x0FFFFFFF) as a 4-byte synchsafe integer.
func appendSynchsafe(b []byte, n uint32) []byte {
	return append(b, byte(n>>21&0x7F), byte(n>>14&0x7F), byte(n>>7&0x7F), byte(n&0x7F))
}

// checkTagSize returns ErrTagTooLarge when a tag body of bodyLen bytes
// (frames and padding) exceeds the synchsafe size limit.
func checkTagSize(bodyLen int) error {
	if bodyLen > maxTagBody {
		return fmt.Errorf("%w: ID3v2 tag body of %d bytes exceeds %d", types.ErrTagTooLarge, bodyLen, maxTagBody)
	}
	return nil
}

// checkFrameSize returns ErrTagTooLarge when a frame payload of n bytes
// cannot be encoded in version's frame header.
func checkFrameSize(n int, version byte) error {
	limit := uint64(maxV24FrameSize)
	if version == 3 {
		limit = maxV23FrameSize
	}
	if uint64(n) > limit { //nolint:gosec // G115: n is a slice length, never negative
		return fmt.Errorf("%w: frame of %d bytes exceeds the ID3v2.%d limit", types.ErrTagTooLarge, n, version)
	}
	return nil
}
