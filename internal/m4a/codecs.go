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
func parseCodecDetails(sr *binary.SafeReader, sampleEntryOffset int64, codec string, file *types.File) error {
	file.Audio.CodecDescription = mapCodecName(codec)

	// For AAC variants, attempt to parse ESDS for profile
	if codec == "mp4a" {
		if profile, err := parseAACProfile(sr, sampleEntryOffset); err == nil && profile != "" {
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
func parseAACProfile(sr *binary.SafeReader, sampleEntryOffset int64) (string, error) {
	// Search for "esds" within sample entry
	searchBuf := make([]byte, 256)
	if err := sr.ReadAt(searchBuf, sampleEntryOffset, "esds search buffer"); err != nil {
		return "", err
	}

	esdsOffset := int64(-1)
	for i := 0; i < len(searchBuf)-4; i++ {
		if string(searchBuf[i:i+4]) == "esds" {
			esdsOffset = sampleEntryOffset + int64(i) - 4
			break
		}
	}

	if esdsOffset == -1 {
		return "", nil
	}

	// Read esds data
	esdsSize, err := binary.Read[uint32](sr, esdsOffset, "esds size")
	if err != nil || esdsSize < 12 || esdsSize > 1024 {
		return "", nil
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

// parseESDescriptors navigates ESDS descriptor hierarchy.
func parseESDescriptors(data []byte) uint8 {
	pos := 0

	readSize := func() int {
		if pos >= len(data) {
			return -1
		}
		size := 0
		for i := 0; i < 4; i++ {
			if pos >= len(data) {
				return -1
			}
			b := data[pos]
			pos++
			size = (size << 7) | int(b&0x7F)
			if (b & 0x80) == 0 {
				break
			}
		}
		return size
	}

	for pos < len(data) {
		if data[pos] == 0x03 {
			pos++
			size := readSize()
			if size < 0 {
				return 0
			}
			pos += 3

			if pos < len(data) && data[pos] == 0x04 {
				pos++
				dcSize := readSize()
				if dcSize < 0 || pos >= len(data) {
					return 0
				}
				return data[pos]
			}
		}
		pos++
	}

	return 0
}
