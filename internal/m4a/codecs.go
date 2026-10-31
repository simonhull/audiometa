// Package m4a provides M4A/M4B format parsing
package m4a

import (
	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
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
	"alac": "Apple Lossless",
	"flac": "FLAC",

	// Other
	"opus": "Opus",
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

	// For AAC variants, attempt to parse ESDS for profile
	if codec == "mp4a" {
		if profile, err := parseAACProfile(sr, sampleEntryOffset, sampleEntrySize); err == nil && profile != "" {
			file.Audio.CodecProfile = profile
			if profile != "AAC-LC" {
				file.Audio.CodecDescription = profile
			}
		}
	}

	// For xHE-AAC, profile is implicit
	if codec == "mhm1" || codec == "mhm2" {
		file.Audio.CodecProfile = "USAC"
	}

	return nil
}

// parseAACProfile attempts to extract AAC profile from ESDS atom.
// The search never reads past the sample entry, so a sample entry that sits
// near end-of-file is still examined.
func parseAACProfile(sr *binary.SafeReader, sampleEntryOffset int64, sampleEntrySize uint32) (string, error) {
	// Search for "esds" within the sample entry (first 256 bytes at most)
	searchLen := min(sampleEntrySize, 256)
	if searchLen < 12 {
		return "", nil
	}
	searchBuf := make([]byte, searchLen)
	if err := sr.ReadAt(searchBuf, sampleEntryOffset, "esds search buffer"); err != nil {
		return "", err
	}

	esdsOffset := int64(-1)
	for i := range len(searchBuf) - 4 {
		if string(searchBuf[i:i+4]) == "esds" {
			esdsOffset = sampleEntryOffset + int64(i) - 4
			break
		}
	}

	if esdsOffset == -1 {
		return "", nil
	}

	// Read esds data
	esdsSize, err := sr.Read[uint32](esdsOffset, "esds size")
	if err != nil || esdsSize < 12 || esdsSize > 1024 {
		return "", nil
	}

	if esdsOffset+int64(esdsSize) > sampleEntryOffset+int64(sampleEntrySize) {
		return "", nil // esds claims to extend past its sample entry
	}

	esdsDataSize := int(esdsSize) - 12
	if esdsDataSize <= 0 || esdsDataSize > 512 {
		return "", nil
	}

	esdsData := make([]byte, esdsDataSize)
	if err := sr.ReadAt(esdsData, esdsOffset+12, "esds data"); err != nil {
		return "", err
	}

	audioObjectType := parseESDescriptors(esdsData)
	if audioObjectType == 0 {
		return "", nil
	}

	if profile, ok := aacProfiles[audioObjectType]; ok {
		return profile, nil
	}

	return "", nil
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

// parseESDescriptors walks ES_Descriptor → DecoderConfigDescriptor →
// DecoderSpecificInfo and returns the audioObjectType from the
// AudioSpecificConfig (ISO/IEC 14496-3 §1.6.2.1). Returns 0 when the
// structure is absent or truncated.
func parseESDescriptors(data []byte) uint8 {
	// ES_Descriptor payload: ES_ID(2), flags(1), then optional fields.
	_, pos, ok := readDescriptorHeader(data, 0, tagESDescriptor)
	if !ok || pos+3 > len(data) {
		return 0
	}
	flags := data[pos+2]
	pos += 3
	if flags&0x80 != 0 { // streamDependenceFlag set: skip the 2-byte dependsOn ES ID
		pos += 2
	}
	if flags&0x40 != 0 { // URL_Flag set: skip the length-prefixed URL
		if pos >= len(data) {
			return 0
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
		return 0
	}

	// DecoderSpecificInfo payload is the AudioSpecificConfig.
	size, pos, ok := readDescriptorHeader(data, pos+13, tagDecoderSpecificInfo)
	if !ok || size == 0 || pos >= len(data) {
		return 0
	}
	return audioObjectType(data[pos:min(pos+size, len(data))])
}
