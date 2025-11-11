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

// Re-export all artwork type constants
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
type RawTag = types.RawTag

// RawTagType is an alias to types.RawTagType for backwards compatibility.
// Re-exporting from internal/types to maintain public API.
type RawTagType = types.RawTagType

// Re-export all raw tag type constants
const (
	RawTagText    = types.RawTagText
	RawTagBinary  = types.RawTagBinary
	RawTagImage   = types.RawTagImage
	RawTagCounter = types.RawTagCounter
	RawTagURL     = types.RawTagURL
)
