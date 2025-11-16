package m4a

import (
	"time"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// parseTechnicalInfo extracts duration, bitrate, sample rate, channels, and codec.
// Returns nil (no error) even if atoms are missing - technical info is best-effort.
func parseTechnicalInfo(sr *binary.SafeReader, moovAtom *Atom, file *types.File) error { //nolint:unparam // Error return kept for consistency with other parsers
	// Find mvhd (movie header) atom for duration
	mvhdAtom, err := findAtom(sr, moovAtom.DataOffset(), moovAtom.DataOffset()+int64(moovAtom.DataSize()), "mvhd")
	if err != nil {
		return nil //nolint:nilerr // Missing atoms are not fatal for technical info parsing
	}

	// Parse mvhd for duration
	if err := parseMvhd(sr, mvhdAtom, file); err != nil {
		return nil //nolint:nilerr // Parse failures are not fatal
	}

	// Find trak atom for audio format info
	// Path: moov -> trak
	trakAtom, err := findAtom(sr, moovAtom.DataOffset(), moovAtom.DataOffset()+int64(moovAtom.DataSize()), "trak")
	if err != nil {
		return nil //nolint:nilerr // Missing atoms are not fatal
	}

	// Find mdia atom
	// Path: trak -> mdia
	mdiaAtom, err := findAtom(sr, trakAtom.DataOffset(), trakAtom.DataOffset()+int64(trakAtom.DataSize()), "mdia")
	if err != nil {
		return nil //nolint:nilerr // Missing atoms are not fatal
	}

	// Find minf atom
	// Path: mdia -> minf
	minfAtom, err := findAtom(sr, mdiaAtom.DataOffset(), mdiaAtom.DataOffset()+int64(mdiaAtom.DataSize()), "minf")
	if err != nil {
		return nil //nolint:nilerr // Missing atoms are not fatal
	}

	// Find stbl atom
	// Path: minf -> stbl
	stblAtom, err := findAtom(sr, minfAtom.DataOffset(), minfAtom.DataOffset()+int64(minfAtom.DataSize()), "stbl")
	if err != nil {
		return nil //nolint:nilerr // Missing atoms are not fatal
	}

	// Find stsd (sample description) atom
	// Path: stbl -> stsd
	stsdAtom, err := findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+int64(stblAtom.DataSize()), "stsd")
	if err != nil {
		return nil //nolint:nilerr // Missing atoms are not fatal
	}

	// Parse stsd for codec, sample rate, channels
	if err := parseStsd(sr, stsdAtom, file); err != nil {
		return nil //nolint:nilerr // Parse failures are not fatal
	}

	// Estimate bitrate if we have duration and file size
	if file.Audio.Duration > 0 && file.Size > 0 {
		durationSec := file.Audio.Duration.Seconds()
		if durationSec > 0 {
			file.Audio.Bitrate = int((float64(file.Size) * 8) / durationSec)
		}
	}

	return nil
}

// parseMvhd parses the movie header atom for duration.
func parseMvhd(sr *binary.SafeReader, mvhdAtom *Atom, file *types.File) error {
	offset := mvhdAtom.DataOffset()

	// Read version (1 byte)
	version, err := binary.Read[uint8](sr, offset, "mvhd version")
	if err != nil {
		return err
	}
	offset++

	// Skip flags (3 bytes)
	offset += 3

	var timescale uint32
	var duration uint64

	if version == 1 {
		timescale, duration, err = parseMvhdVersion1(sr, offset)
	} else {
		timescale, duration, err = parseMvhdVersion0(sr, offset)
	}

	if err != nil {
		return err
	}

	// Calculate duration: duration / timescale
	if timescale > 0 {
		durationNs := (int64(duration) * 1_000_000_000) / int64(timescale)
		file.Audio.Duration = time.Duration(durationNs)
	}

	return nil
}

// parseMvhdVersion0 parses 32-bit mvhd (version 0).
func parseMvhdVersion0(sr *binary.SafeReader, offset int64) (timescale uint32, duration uint64, err error) {
	// Skip creation time (4 bytes) and modification time (4 bytes)
	offset += 8

	// Read timescale (4 bytes)
	timescale, err = binary.Read[uint32](sr, offset, "mvhd timescale")
	if err != nil {
		return 0, 0, err
	}
	offset += 4

	// Read duration (4 bytes)
	duration32, err := binary.Read[uint32](sr, offset, "mvhd duration")
	if err != nil {
		return 0, 0, err
	}

	return timescale, uint64(duration32), nil
}

// parseMvhdVersion1 parses 64-bit mvhd (version 1).
func parseMvhdVersion1(sr *binary.SafeReader, offset int64) (timescale uint32, duration uint64, err error) {
	// Skip creation time (8 bytes) and modification time (8 bytes)
	offset += 16

	// Read timescale (4 bytes)
	timescale, err = binary.Read[uint32](sr, offset, "mvhd timescale")
	if err != nil {
		return 0, 0, err
	}
	offset += 4

	// Read duration (8 bytes)
	duration, err = binary.Read[uint64](sr, offset, "mvhd duration")
	if err != nil {
		return 0, 0, err
	}

	return timescale, duration, nil
}

// parseStsd parses the sample description atom for codec, sample rate, channels.
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

	// Parse enhanced codec details (non-fatal if it fails)
	_ = parseCodecDetails(sr, offset-8, codec, file) //nolint:errcheck // Enhanced details are optional

	// Skip reserved (6 bytes) and data reference index (2 bytes)
	offset += 8

	// Audio sample entry specific fields:
	// Skip version (2 bytes), revision level (2 bytes), and vendor (4 bytes)
	offset += 8

	// [2 bytes] number of channels
	channels, err := binary.Read[uint16](sr, offset, "channels")
	if err != nil {
		return err
	}
	file.Audio.Channels = int(channels)
	offset += 2

	// Skip sample size (2 bytes - bits per sample)
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

	return nil
}
