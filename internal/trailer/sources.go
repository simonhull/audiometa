package trailer

import "github.com/simonhull/audiometa/internal/types"

// AddSources records the tag blocks on f, in the order [types.TagSource]
// lists them. An ID3v1 trailer comes first, as the block a reader consults; it
// is used when id3v1Used says it supplied values, and only the last block of
// the file can be. The other blocks follow in file order, unread.
func AddSources(f *types.File, blocks []Block, id3v1Used bool) {
	for i, b := range blocks {
		if b.Kind == KindID3v1 {
			types.AddTagSource(f, types.TagSource{Kind: types.TagID3v1, Used: id3v1Used && i == len(blocks)-1})
		}
	}
	for _, b := range blocks {
		switch b.Kind {
		case KindAPEv2:
			types.AddTagSource(f, types.TagSource{Kind: types.TagAPEv2})
		case KindLyrics3:
			types.AddTagSource(f, types.TagSource{Kind: types.TagLyrics3})
		case KindID3v2: // only ID3v2.4 has a footer
			types.AddTagSource(f, types.TagSource{Kind: types.TagID3v2, Version: 4})
		case KindID3v1: // listed above
		}
	}
}
