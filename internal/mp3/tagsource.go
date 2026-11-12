package mp3

import (
	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// id3v2Source describes the ID3v2 tag at the start of sr. Its version is the
// major version byte as stored, and parseErr is what parsing it returned.
func id3v2Source(sr *binutil.SafeReader, parseErr error) types.TagSource {
	var version [1]byte
	if sr.ReadAt(version[:], 3, "ID3v2 version") != nil {
		version[0] = 0
	}
	return types.TagSource{Kind: types.TagID3v2, Version: int(version[0]), Used: parseErr == nil}
}
