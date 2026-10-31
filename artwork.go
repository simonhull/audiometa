package audiometa

import (
	"github.com/simonhull/audiometa/internal/types"
)

// Artwork is an alias to types.Artwork for backwards compatibility.
// Re-exporting from internal/types to maintain public API.
type Artwork = types.Artwork

// ArtworkType is an alias to types.ArtworkType for backwards compatibility.
// Re-exporting from internal/types to maintain public API.
type ArtworkType = types.ArtworkType

// Re-export all artwork type constants.
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

// RawTag is an alias to types.RawTag for backwards compatibility.
// Re-exporting from internal/types to maintain public API.
//
// Deprecated: raw tags are exposed via Tags.Get and Tags.All; RawTag is never produced.
type RawTag = types.RawTag

// RawTagType is an alias to types.RawTagType for backwards compatibility.
// Re-exporting from internal/types to maintain public API.
//
// Deprecated: raw tags are exposed via Tags.Get and Tags.All; RawTagType is never produced.
type RawTagType = types.RawTagType

// Re-export all raw tag type constants.
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
