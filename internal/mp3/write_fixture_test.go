package mp3

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// id3TagFlags is id3Tag with header flags and trailing zero padding.
func id3TagFlags(version, flags byte, padding int, frames ...[]byte) []byte {
	body := append(bytes.Join(frames, nil), make([]byte, padding)...)
	h := append([]byte{'I', 'D', '3', version, 0, flags}, synchsafe(len(body))...)
	return append(h, body...)
}

// id3FrameFlags is id3Frame with frame flags.
func id3FrameFlags(version byte, id string, flags uint16, payload []byte) []byte {
	f := id3Frame(version, id, payload)
	binary.BigEndian.PutUint16(f[8:10], flags)
	return f
}

// txxxPayload builds a TXXX payload in encoding 0 (Latin-1) or 3 (UTF-8).
func txxxPayload(enc byte, desc, value string) []byte {
	b := append([]byte{enc}, desc...)
	b = append(b, 0)
	return append(b, value...)
}

// apicPayload builds a Latin-1 APIC payload.
func apicPayload(mime string, picType byte, desc string, data []byte) []byte {
	b := append([]byte{0}, mime...)
	b = append(b, 0, picType)
	b = append(b, desc...)
	b = append(b, 0)
	return append(b, data...)
}

// commPayload builds a Latin-1 COMM payload.
func commPayload(lang, desc, text string) []byte {
	b := append([]byte{0}, lang...)
	b = append(b, desc...)
	b = append(b, 0)
	return append(b, text...)
}

// writeBytes runs the writer over src and returns its output.
func writeBytes(t *testing.T, src []byte, edits *types.EditSet) []byte {
	t.Helper()
	out, err := tryWrite(src, edits)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return out
}

// tryWrite runs the writer over src.
func tryWrite(src []byte, edits *types.EditSet) ([]byte, error) {
	var buf bytes.Buffer
	n, err := (&writer{}).WriteFile(context.Background(), &buf, bytes.NewReader(src), int64(len(src)), edits)
	if err == nil && n != int64(buf.Len()) {
		panic("WriteFile miscounted its output")
	}
	return buf.Bytes(), err
}

// boundTags parses src and binds its tags to the MP3 format.
func boundTags(t *testing.T, src []byte) *types.Tags {
	t.Helper()
	tags := parseBytes(t, src).Tags
	types.BindFormat(&tags, types.FormatMP3)
	return &tags
}

// editsFor builds the EditSet that mutate (applied to src's tags) produces,
// plus the given artwork operations.
func editsFor(t *testing.T, src []byte, mutate func(*types.Tags), art ...types.ArtworkOp) *types.EditSet {
	t.Helper()
	orig := boundTags(t, src)
	cur := orig.Clone()
	types.BindFormat(cur, types.FormatMP3)
	if mutate != nil {
		mutate(cur)
	}
	keys, fields, err := types.Diff(orig, cur, types.FormatMP3)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	return &types.EditSet{Keys: keys, Fields: fields, Artwork: art, Orig: orig, Final: cur}
}

// framesOf reads the frame list of data's ID3v2 tag.
func framesOf(t *testing.T, data []byte) *wtag {
	t.Helper()
	wt, err := readWTag(context.Background(), binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "t.mp3"), int64(len(data)))
	if err != nil {
		t.Fatalf("readWTag: %v", err)
	}
	return wt
}

// frameIDs lists the IDs of a frame list.
func frameIDs(frames []wframe) []string {
	ids := make([]string, len(frames))
	for i, f := range frames {
		ids[i] = f.id
	}
	return ids
}
