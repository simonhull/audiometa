package m4a

import (
	"time"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// parseTechnicalInfo extracts duration, bitrate, sample rate, channels, and codec
func parseTechnicalInfo(sr *binary.SafeReader, moovAtom *Atom, file *types.File) error {
	// Find mvhd (movie header) atom for duration
	mvhdAtom, err := findAtom(sr, moovAtom.DataOffset(), moovAtom.DataOffset()+int64(moovAtom.DataSize()), "mvhd")
	if err != nil {
		// No mvhd - not fatal, just skip duration parsing
		return nil
	}

	// Parse mvhd for duration
	if err := parseMvhd(sr, mvhdAtom, file); err != nil {
		// Non-fatal
		return nil
	}

	// Find trak atom for audio format info
	// Path: moov -> trak
	trakAtom, err := findAtom(sr, moovAtom.DataOffset(), moovAtom.DataOffset()+int64(moovAtom.DataSize()), "trak")
	if err != nil {
		// No trak - not fatal
		return nil
	}

	// Find mdia atom
	// Path: trak -> mdia
	mdiaAtom, err := findAtom(sr, trakAtom.DataOffset(), trakAtom.DataOffset()+int64(trakAtom.DataSize()), "mdia")
	if err != nil {
		return nil
	}

	// Find minf atom
	// Path: mdia -> minf
	minfAtom, err := findAtom(sr, mdiaAtom.DataOffset(), mdiaAtom.DataOffset()+int64(mdiaAtom.DataSize()), "minf")
	if err != nil {
		return nil
	}

	// Find stbl atom
	// Path: minf -> stbl
	stblAtom, err := findAtom(sr, minfAtom.DataOffset(), minfAtom.DataOffset()+int64(minfAtom.DataSize()), "stbl")
	if err != nil {
		return nil
	}

	// Find stsd (sample description) atom
	// Path: stbl -> stsd
	stsdAtom, err := findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+int64(stblAtom.DataSize()), "stsd")
	if err != nil {
		return nil
	}

	// Parse stsd for codec, sample rate, channels
	if err := parseStsd(sr, stsdAtom, file); err != nil {
		// Non-fatal
		return nil
	}

	// Estimate bitrate if we have duration and file size
	if file.Audio.Duration > 0 && file.Size > 0 {
		// bitrate = (fileSize * 8) / duration_in_seconds
		durationSec := file.Audio.Duration.Seconds()
		if durationSec > 0 {
			file.Audio.Bitrate = int((float64(file.Size) * 8) / durationSec)
		}
	}

	return nil
}

// parseMvhd parses the movie header atom for duration
func parseMvhd(sr *binary.SafeReader, mvhdAtom *Atom, file *types.File) error {
	offset := mvhdAtom.DataOffset()

	// Read version (1 byte)
	version, err := binary.Read[uint8](sr, offset, "mvhd version")
	if err != nil {
		return err
	}
	offset += 1

	// Skip flags (3 bytes)
	offset += 3

	var timescale, duration uint32

	if version == 1 {
		// 64-bit version
		// Skip creation time (8 bytes) and modification time (8 bytes)
		offset += 16

		// Read timescale (4 bytes)
		timescale, err = binary.Read[uint32](sr, offset, "mvhd timescale")
		if err != nil {
			return err
		}
		offset += 4

		// Read duration (8 bytes)
		duration64, err := binary.Read[uint64](sr, offset, "mvhd duration")
		if err != nil {
			return err
		}
		duration = uint32(duration64) // Truncate for now (should be safe for audio files)
	} else {
		// 32-bit version (version == 0)
		// Skip creation time (4 bytes) and modification time (4 bytes)
		offset += 8

		// Read timescale (4 bytes)
		timescale, err = binary.Read[uint32](sr, offset, "mvhd timescale")
		if err != nil {
			return err
		}
		offset += 4

		// Read duration (4 bytes)
		duration, err = binary.Read[uint32](sr, offset, "mvhd duration")
		if err != nil {
			return err
		}
	}

	// Calculate duration: duration / timescale
	if timescale > 0 {
		durationNs := (int64(duration) * 1_000_000_000) / int64(timescale)
		file.Audio.Duration = time.Duration(durationNs)
	}

	return nil
}

// parseStsd parses the sample description atom for codec, sample rate, channels
func parseStsd(sr *binary.SafeReader, stsdAtom *Atom, file *types.File) error {
	offset := stsdAtom.DataOffset()

	// stsd structure:
	// [1 byte]  version
	// [3 bytes] flags
	// [4 bytes] number of entries

	// Skip version + flags
	offset += 4

	// Read number of entries
	numEntries, err := binary.Read[uint32](sr, offset, "stsd entry count")
	if err != nil {
		return err
	}
	offset += 4

	if numEntries == 0 {
		return nil
	}

	// Read first entry (audio sample description)
	// Entry structure:
	// [4 bytes] size
	// [4 bytes] format (codec type)
	// ... more fields ...

	_, err = binary.Read[uint32](sr, offset, "stsd entry size")
	if err != nil {
		return err
	}
	offset += 4

	// Read format (codec type) - 4 bytes
	formatBytes := make([]byte, 4)
	if err := sr.ReadAt(formatBytes, offset, "stsd format"); err != nil {
		return err
	}
	offset += 4

	codec := string(formatBytes)
	file.Audio.Codec = codec

	// Skip reserved (6 bytes) and data reference index (2 bytes)
	offset += 8

	// Audio sample entry specific fields:
	// [2 bytes] version (usually 0 or 1)
	// [2 bytes] revision level
	// [4 bytes] vendor
	audioVersion, err := binary.Read[uint16](sr, offset, "audio version")
	if err != nil {
		return err
	}
	offset += 2

	// Skip revision level (2 bytes) and vendor (4 bytes)
	offset += 6

	// [2 bytes] number of channels
	channels, err := binary.Read[uint16](sr, offset, "channels")
	if err != nil {
		return err
	}
	file.Audio.Channels = int(channels)
	offset += 2

	// [2 bytes] sample size (bits per sample)
	// sampleSize, _ := binary.Read[uint16](sr, offset, "sample size")
	offset += 2

	// Skip compression ID (2 bytes) and packet size (2 bytes)
	offset += 4

	// [4 bytes] sample rate (16.16 fixed point)
	sampleRateFixed, err := binary.Read[uint32](sr, offset, "sample rate")
	if err != nil {
		return err
	}

	// Convert 16.16 fixed point to integer
	// High 16 bits = integer part, low 16 bits = fractional part
	file.Audio.SampleRate = int(sampleRateFixed >> 16)

	// For version 1 and 2, there are additional fields, but we'll skip them for now

	_ = audioVersion // Unused for now

	return nil
}
