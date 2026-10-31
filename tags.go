package audiometa

import (
	"github.com/simonhull/audiometa/internal/types"
)

// Tags holds a file's metadata in format-agnostic form.
//
// Standard fields (Title, Artist, Album, AlbumArtist, Year, TrackNumber,
// Narrator, Series, SeriesPart, ISBN, ASIN, MusicBrainz IDs and more) are
// filled from whichever native tags the format uses. Multi-valued tags appear
// in the slice fields Artists, Genres, Composers and Performers. Empty or zero
// fields were not present in the file.
//
// Every tag is also kept under its native key: use Tags.Get, Tags.GetFirst
// or Tags.GetBest to look one up and Tags.All to iterate over them. Raw
// keys are format-specific:
//
//   - FLAC, Ogg Vorbis and Opus: the Vorbis comment field name, upper-cased
//     ("TITLE", "ENCODER").
//   - MP3: the ID3v2 frame ID ("TIT2", "TSSE"); TXXX frames are stored under
//     their upper-cased description.
//   - M4A/M4B: the ilst atom name ("\xa9nam" for ©nam, "aART"); freeform
//     (----) atoms are stored under their upper-cased name.
//
// Every field and method is documented on the aliased type, [types.Tags].
type Tags = types.Tags
