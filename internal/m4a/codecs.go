package m4a

import (
	"math"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// Sample entry FourCCs of the lossless codecs. FLAC's is spelled "fLaC" in files.
const (
	codecALAC = "alac"
	codecFLAC = "fLaC"
)

// codecNames maps MP4 codec FourCC codes to human-readable names.
var codecNames = map[string]string{
	// AAC Family
	"mp4a": "AAC",
	"mhm1": "xHE-AAC",
	"mhm2": "xHE-AAC v2",

	// Dolby Family
	"ac-3": "AC-3",
	"ec-3": "E-AC-3",
	"ac-4": "AC-4",

	// Lossless
	codecALAC: "Apple Lossless",
	codecFLAC: "FLAC",

	// Other
	"Opus": "Opus",
	"mp3 ": "MP3",
	".mp3": "MP3",
}

// aacProfiles maps AAC Audio Object Types to profile names.
var aacProfiles = map[uint8]string{
	1:  "AAC Main",
	2:  "AAC-LC",
	3:  "AAC-SSR",
	4:  "AAC-LTP",
	5:  "HE-AAC",
	6:  "AAC Scalable",
	29: "HE-AAC v2",
	42: "xHE-AAC",
}

// mapCodecName converts a FourCC codec identifier to a human-readable name.
func mapCodecName(fourCC string) string {
	if name, ok := codecNames[fourCC]; ok {
		return name
	}
	return fourCC
}

// parseCodecDetails enriches codec information with human-readable names.
func parseCodecDetails(sr *binary.SafeReader, sampleEntryOffset int64, sampleEntrySize uint32, codec string, file *types.File) error {
	file.Audio.CodecDescription = mapCodecName(codec)

	// For AAC variants, attempt to parse ESDS for profile and bitrate
	if codec == "mp4a" {
		if es, err := parseESDS(sr, sampleEntryOffset, sampleEntrySize); err == nil {
			applyESDescriptor(file, es)
		}
	}

	// For xHE-AAC, profile is implicit
	if codec == "mhm1" || codec == "mhm2" {
		file.Audio.CodecProfile = "USAC"
	}

	return nil
}

// applyESDescriptor records the AAC profile and declared bitrate of es.
func applyESDescriptor(file *types.File, es esDescriptor) {
	if profile := aacProfiles[es.objectType]; profile != "" {
		file.Audio.CodecProfile = profile
		if profile != "AAC-LC" {
			file.Audio.CodecDescription = profile
		}
	}
	if es.avgBitrate > 0 && es.avgBitrate <= math.MaxInt32 {
		file.Audio.Bitrate = int(es.avgBitrate)
	}
}

// parseESDS reads the esds atom of an mp4a sample entry: the audio object type
// and the declared average bitrate.
// The search never reads past the sample entry, so a sample entry that sits
// near end-of-file is still examined.
func parseESDS(sr *binary.SafeReader, sampleEntryOffset int64, sampleEntrySize uint32) (esDescriptor, error) {
	// Search for "esds" within the sample entry (first 256 bytes at most)
	searchLen := min(sampleEntrySize, 256)
	if searchLen < 12 {
		return esDescriptor{}, nil
	}
	searchBuf := make([]byte, searchLen)
	if err := sr.ReadAt(searchBuf, sampleEntryOffset, "esds search buffer"); err != nil {
		return esDescriptor{}, err
	}

	esdsOffset := int64(-1)
	for i := range len(searchBuf) - 4 {
		if string(searchBuf[i:i+4]) == "esds" {
			esdsOffset = sampleEntryOffset + int64(i) - 4
			break
		}
	}

	if esdsOffset == -1 {
		return esDescriptor{}, nil
	}

	// Read esds data
	esdsSize, err := sr.Read[uint32](esdsOffset, "esds size")
	if err != nil || esdsSize < 12 || esdsSize > 1024 {
		return esDescriptor{}, nil
	}

	if esdsOffset+int64(esdsSize) > sampleEntryOffset+int64(sampleEntrySize) {
		return esDescriptor{}, nil // esds claims to extend past its sample entry
	}

	esdsDataSize := int(esdsSize) - 12
	if esdsDataSize <= 0 || esdsDataSize > 512 {
		return esDescriptor{}, nil
	}

	esdsData := make([]byte, esdsDataSize)
	if err := sr.ReadAt(esdsData, esdsOffset+12, "esds data"); err != nil {
		return esDescriptor{}, err
	}

	return parseESDescriptors(esdsData), nil
}

// MPEG-4 descriptor tags used inside an esds atom (ISO/IEC 14496-1 §7.2.2.1).
const (
	tagESDescriptor            = 0x03
	tagDecoderConfigDescriptor = 0x04
	tagDecoderSpecificInfo     = 0x05
)

// readDescriptorHeader reads an MPEG-4 descriptor tag at pos and its
// variable-length size (1-4 bytes, 7 bits each). It returns the payload
// size and the position of the payload, or ok=false if the tag differs or
// data runs out.
func readDescriptorHeader(data []byte, pos int, want byte) (size, payload int, ok bool) {
	if pos < 0 || pos >= len(data) || data[pos] != want {
		return 0, 0, false
	}
	pos++
	for range 4 {
		if pos >= len(data) {
			return 0, 0, false
		}
		b := data[pos]
		pos++
		size = size<<7 | int(b&0x7F)
		if b&0x80 == 0 {
			break
		}
	}
	return size, pos, true
}

// audioObjectType decodes the leading audioObjectType of an
// AudioSpecificConfig: 5 bits, where 31 escapes to 32 + the next 6 bits.
func audioObjectType(asc []byte) uint8 {
	if len(asc) == 0 {
		return 0
	}
	aot := asc[0] >> 3
	if aot != 31 {
		return aot
	}
	if len(asc) < 2 {
		return 0
	}
	return 32 + ((asc[0]&0x07)<<3 | asc[1]>>5)
}

// esDescriptor holds what audiometa uses from an esds atom.
type esDescriptor struct {
	objectType uint8  // AudioSpecificConfig audioObjectType; 0 when absent
	avgBitrate uint32 // DecoderConfigDescriptor avgBitrate in bit/s; 0 when unknown
}

// parseESDescriptors walks ES_Descriptor → DecoderConfigDescriptor →
// DecoderSpecificInfo and returns the avgBitrate of the
// DecoderConfigDescriptor and the audioObjectType from the
// AudioSpecificConfig (ISO/IEC 14496-3 §1.6.2.1). Fields of a structure that
// is absent or truncated are 0.
func parseESDescriptors(data []byte) esDescriptor {
	var es esDescriptor
	// ES_Descriptor payload: ES_ID(2), flags(1), then optional fields.
	_, pos, ok := readDescriptorHeader(data, 0, tagESDescriptor)
	if !ok || pos+3 > len(data) {
		return es
	}
	flags := data[pos+2]
	pos += 3
	if flags&0x80 != 0 { // streamDependenceFlag set: skip the 2-byte dependsOn ES ID
		pos += 2
	}
	if flags&0x40 != 0 { // URL_Flag set: skip the length-prefixed URL
		if pos >= len(data) {
			return es
		}
		pos += 1 + int(data[pos])
	}
	if flags&0x20 != 0 { // OCRstreamFlag set: skip the 2-byte OCR ES ID
		pos += 2
	}

	// DecoderConfigDescriptor payload starts with objectTypeIndication(1),
	// streamType(1), bufferSizeDB(3), maxBitrate(4), avgBitrate(4) = 13 bytes.
	_, pos, ok = readDescriptorHeader(data, pos, tagDecoderConfigDescriptor)
	if !ok {
		return es
	}
	if pos+13 <= len(data) {
		es.avgBitrate = uint32(data[pos+9])<<24 | uint32(data[pos+10])<<16 | uint32(data[pos+11])<<8 | uint32(data[pos+12])
	}

	// DecoderSpecificInfo payload is the AudioSpecificConfig.
	size, pos, ok := readDescriptorHeader(data, pos+13, tagDecoderSpecificInfo)
	if !ok || size == 0 || pos >= len(data) {
		return es
	}
	es.objectType = audioObjectType(data[pos:min(pos+size, len(data))])
	return es
}
