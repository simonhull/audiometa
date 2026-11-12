package ogg

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"slices"

	"github.com/simonhull/audiometa/internal/picture"
	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/vorbis"
)

// pictureKey is the comment key that carries a picture: the base64 encoding of
// a FLAC PICTURE block (see [parseMetadataBlockPicture]).
const pictureKey = "METADATA_BLOCK_PICTURE"

// isPictureEntry reports whether the raw comment entry has the picture key
// (case-insensitively).
func isPictureEntry(entry []byte) bool {
	key, _, ok := bytes.Cut(entry, []byte{'='})
	return ok && string(bytes.ToUpper(key)) == pictureKey
}

// pictureEntryType returns the picture type of a picture entry and whether it
// is readable: only the start of the base64 value is decoded.
func pictureEntryType(entry []byte) (types.ArtworkType, bool) {
	_, value, _ := bytes.Cut(entry, []byte{'='})
	const prefix = 8 // eight base64 characters decode to the six bytes that hold the type
	if len(value) < prefix {
		return 0, false
	}
	var block [6]byte
	if _, err := base64.StdEncoding.Decode(block[:], value[:prefix]); err != nil {
		return 0, false
	}
	return types.ArtworkTypeFromPictureType(picture.Type(block[:])), true
}

// applyArtwork applies the artwork operations, in order, to the picture
// entries of v. A new picture goes after the last picture entry, or at the end
// when there is none. Removing by type leaves pictures that do not decode, as
// the reader does not report them either; removing all drops every picture
// entry.
func applyArtwork(v *vorbis.Editor, ops []types.ArtworkOp) error {
	for _, op := range ops {
		switch op.Kind {
		case types.ArtworkOpAdd:
			block, err := picture.Encode(op.Art)
			if err != nil {
				return err
			}
			entry := []byte(pictureKey + "=" + base64.StdEncoding.EncodeToString(block))
			at := len(v.Entries)
			if i := lastIndexFunc(v.Entries, isPictureEntry); i >= 0 {
				at = i + 1
			}
			v.Entries = slices.Insert(v.Entries, at, entry)
		case types.ArtworkOpRemove:
			v.Entries = slices.DeleteFunc(v.Entries, func(e []byte) bool {
				typ, ok := pictureEntryType(e)
				return isPictureEntry(e) && ok && typ == op.Type
			})
		case types.ArtworkOpRemoveAll:
			v.Entries = slices.DeleteFunc(v.Entries, isPictureEntry)
		default:
			return fmt.Errorf("%w: unknown artwork operation %d", types.ErrUnsupportedWrite, op.Kind)
		}
	}
	return nil
}

// lastIndexFunc returns the index of the last element of s for which match is
// true, or -1.
func lastIndexFunc[E any](s []E, match func(E) bool) int {
	for i, e := range slices.Backward(s) {
		if match(e) {
			return i
		}
	}
	return -1
}
