package audiometa

import (
	"github.com/simonhull/audiometa/internal/types"
)

// Artwork is an image embedded in an audio file, as returned by
// [File.ExtractArtwork].
//
// Data holds the encoded image bytes, MIMEType their type (for example
// "image/jpeg"), Type the picture's role ([ArtworkFrontCover] and so on) and
// Description the embedded description, if any. Width and Height are the
// pixel dimensions, taken from the picture block (FLAC, Ogg Vorbis, Opus) or
// read from JPEG and PNG data (MP3, M4A); they are 0 when unknown.
type Artwork = types.Artwork

// ArtworkType is the role of an embedded picture, using the picture type
// codes shared by ID3v2 APIC frames and FLAC PICTURE blocks. M4A has no
// picture types, so its covr images are reported as [ArtworkFrontCover].
type ArtworkType = types.ArtworkType

// Artwork types, numbered as in the ID3v2 APIC and FLAC PICTURE picture type
// field.
const (
	ArtworkOther             = types.ArtworkOther
	ArtworkIcon              = types.ArtworkIcon
	ArtworkOtherIcon         = types.ArtworkOtherIcon
	ArtworkFrontCover        = types.ArtworkFrontCover
	ArtworkBackCover         = types.ArtworkBackCover
	ArtworkLeaflet           = types.ArtworkLeaflet
	ArtworkMedia             = types.ArtworkMedia
	ArtworkLeadArtist        = types.ArtworkLeadArtist
	ArtworkArtist            = types.ArtworkArtist
	ArtworkConductor         = types.ArtworkConductor
	ArtworkBand              = types.ArtworkBand
	ArtworkComposer          = types.ArtworkComposer
	ArtworkLyricist          = types.ArtworkLyricist
	ArtworkRecordingLocation = types.ArtworkRecordingLocation
	ArtworkDuringRecording   = types.ArtworkDuringRecording
	ArtworkDuringPerformance = types.ArtworkDuringPerformance
	ArtworkVideoCapture      = types.ArtworkVideoCapture
	ArtworkBrightFish        = types.ArtworkBrightFish
	ArtworkIllustration      = types.ArtworkIllustration
	ArtworkBandLogotype      = types.ArtworkBandLogotype
	ArtworkPublisherLogotype = types.ArtworkPublisherLogotype
)

// RawTag was intended to describe an unparsed tag value.
//
// Deprecated: raw tags are exposed via Tags.Get and Tags.All; RawTag is never produced.
type RawTag = types.RawTag

// RawTagType was intended to classify a [RawTag] value.
//
// Deprecated: raw tags are exposed via Tags.Get and Tags.All; RawTagType is never produced.
type RawTagType = types.RawTagType

// Raw tag types. All of them are deprecated.
const (
	// RawTagText is an alias to types.RawTagText.
	//
	// Deprecated: raw tags are exposed via Tags.Get and Tags.All; RawTagText is never produced.
	RawTagText = types.RawTagText
	// RawTagBinary is an alias to types.RawTagBinary.
	//
	// Deprecated: raw tags are exposed via Tags.Get and Tags.All; RawTagBinary is never produced.
	RawTagBinary = types.RawTagBinary
	// RawTagImage is an alias to types.RawTagImage.
	//
	// Deprecated: raw tags are exposed via Tags.Get and Tags.All; RawTagImage is never produced.
	RawTagImage = types.RawTagImage
	// RawTagCounter is an alias to types.RawTagCounter.
	//
	// Deprecated: raw tags are exposed via Tags.Get and Tags.All; RawTagCounter is never produced.
	RawTagCounter = types.RawTagCounter
	// RawTagURL is an alias to types.RawTagURL.
	//
	// Deprecated: raw tags are exposed via Tags.Get and Tags.All; RawTagURL is never produced.
	RawTagURL = types.RawTagURL
)
