package ogg

import (
	"context"
	"errors"

	"github.com/simonhull/audiometa/internal/types"
)

// vorbisMagic is the packet magic that follows the packet-type byte of every Vorbis header.
const vorbisMagic = "vorbis"

// errUnknownCodec is returned for a first packet that is neither Vorbis nor Opus.
var errUnknownCodec = errors.New(`unknown or unsupported Ogg codec: "unknown"`)

// codec is one Ogg mapping audiometa reads: how to recognize its first packet and how to
// read its headers.
type codec struct {
	format        types.Format
	headerPackets int    // header packets before the audio (Vorbis I §4.2, RFC 7845 §3)
	idName        string // names the identification header in errors
	commentName   string // names the comment header in warnings
	parseID       func(packet []byte, file *types.File) error
	parseComments func(ctx context.Context, packet []byte, file *types.File) error
}

var (
	vorbisCodec = &codec{
		format:        types.FormatOgg,
		headerPackets: 3, // identification, comment and setup
		idName:        "Vorbis identification header",
		commentName:   "Vorbis comment header",
		parseID:       parseVorbisIdentification,
		parseComments: parseVorbisComment,
	}
	opusCodec = &codec{
		format:        types.FormatOpus,
		headerPackets: 2, // OpusHead and OpusTags
		idName:        "OpusHead header",
		commentName:   "OpusTags header",
		parseID:       parseOpusHead,
		parseComments: parseOpusTags,
	}
)

// detectCodec returns the codec whose identification packet starts firstPacket, or nil.
func detectCodec(firstPacket []byte) *codec {
	if len(firstPacket) >= 8 && string(firstPacket[0:8]) == "OpusHead" {
		return opusCodec
	}
	if len(firstPacket) >= 7 && firstPacket[0] == 0x01 && string(firstPacket[1:7]) == vorbisMagic {
		return vorbisCodec
	}
	return nil
}
