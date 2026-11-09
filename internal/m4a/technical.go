package m4a

import (
	"context"
	"math"
	"time"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// parseTechnicalInfo extracts duration, bitrate, sample rate, channels, and codec.
// Returns nil (no error) even if atoms are missing - technical info is best-effort.
func parseTechnicalInfo(ctx context.Context, sr *binary.SafeReader, moovAtom *Atom, file *types.File) error {
	// Find mvhd (movie header) atom for duration
	mvhdAtom, err := findAtom(sr, moovAtom.DataOffset(), moovAtom.DataOffset()+moovAtom.DataSize(), "mvhd")
	if err != nil {
		return nil
	}

	// Parse mvhd for duration
	if err := parseMvhd(sr, mvhdAtom, file); err != nil {
		return nil
	}

	// Find the audio trak for format info
	// Path: moov -> trak (handler type "soun")
	trakAtom := selectAudioTrak(sr, moovAtom)
	if trakAtom == nil {
		return nil
	}

	// Find mdia atom
	// Path: trak -> mdia
	mdiaAtom, err := findAtom(sr, trakAtom.DataOffset(), trakAtom.DataOffset()+trakAtom.DataSize(), "mdia")
	if err != nil {
		return nil
	}

	// Find minf atom
	// Path: mdia -> minf
	minfAtom, err := findAtom(sr, mdiaAtom.DataOffset(), mdiaAtom.DataOffset()+mdiaAtom.DataSize(), "minf")
	if err != nil {
		return nil
	}

	// Find stbl atom
	// Path: minf -> stbl
	stblAtom, err := findAtom(sr, minfAtom.DataOffset(), minfAtom.DataOffset()+minfAtom.DataSize(), "stbl")
	if err != nil {
		return nil
	}

	// Find stsd (sample description) atom
	// Path: stbl -> stsd
	stsdAtom, err := findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+stblAtom.DataSize(), "stsd")
	if err != nil {
		return nil
	}

	// Parse stsd for codec, sample rate, channels
	if err := parseStsd(sr, stsdAtom, file); err != nil {
		return nil
	}

	// The esds avgBitrate, when the sample description declared one, is already
	// in file.Audio.Bitrate. Otherwise the track's own sample sizes, never the
	// file size (cover art and other atoms are not audio).
	if file.Audio.Bitrate == 0 {
		if n, ok := sampleBytes(ctx, sr, stblAtom, file.Size); ok {
			file.Audio.Bitrate = types.AverageBitrate(n, file.Audio.Duration)
		}
	}

	return nil
}

// stszChunk is how many stsz table entries sampleBytes reads at a time.
const stszChunk = 4096

// sampleBytes returns the total size of a track's samples from its stsz
// atom (ISO/IEC 14496-12 §8.7.3). ok is false when stsz is absent,
// unreadable, or claims more bytes than limit (the file size, when known):
// a table no file could hold is not believed.
func sampleBytes(ctx context.Context, sr *binary.SafeReader, stbl *Atom, limit int64) (int64, bool) {
	stsz, err := findAtom(sr, stbl.DataOffset(), stbl.DataOffset()+stbl.DataSize(), "stsz")
	if err != nil || stsz.DataSize() < 12 {
		return 0, false
	}
	off := stsz.DataOffset() + 4 // version and flags
	defaultSize, err := sr.Read[uint32](off, "stsz default sample size")
	if err != nil {
		return 0, false
	}
	count, err := sr.Read[uint32](off+4, "stsz sample count")
	if err != nil {
		return 0, false
	}
	maxBytes := uint64(math.MaxInt64)
	if limit > 0 {
		maxBytes = uint64(limit)
	}
	if defaultSize != 0 {
		total := uint64(defaultSize) * uint64(count) // < 2^64: both factors < 2^32
		return int64(min(total, maxBytes)), total <= maxBytes
	}
	if int64(count) > (stsz.DataSize()-12)/4 {
		return 0, false // the table would run past its atom
	}

	buf := make([]byte, 4*min(count, stszChunk))
	var total uint64
	pos := off + 8
	for i, left := 0, count; left > 0; i++ {
		if binary.Canceled(ctx, i) != nil {
			return 0, false
		}
		n := min(left, stszChunk)
		chunk := buf[:4*n]
		if err := sr.ReadAt(chunk, pos, "stsz sample sizes"); err != nil {
			return 0, false
		}
		for j := 0; j < len(chunk); j += 4 {
			total += uint64(chunk[j])<<24 | uint64(chunk[j+1])<<16 | uint64(chunk[j+2])<<8 | uint64(chunk[j+3])
		}
		if total > maxBytes {
			return 0, false
		}
		pos += int64(4 * n)
		left -= n
	}
	return int64(total), true
}

// handlerSound is the hdlr handler type of an audio track.
const handlerSound = "soun"

// selectAudioTrak returns the first trak under moov whose handler type is
// "soun". Files whose first track is something else (a chapter text track, say)
// would otherwise yield channels and sample rate from the wrong track. If no
// track declares a sound handler, it falls back to the first trak.
func selectAudioTrak(sr *binary.SafeReader, moovAtom *Atom) *Atom {
	offset := moovAtom.DataOffset()
	end := offset + moovAtom.DataSize()

	var first *Atom
	for offset < end {
		atom, err := readAtomHeader(sr, offset, end)
		if err != nil {
			break
		}
		offset += atom.Size
		if atom.Type != atomTrak {
			continue
		}
		if first == nil {
			first = atom
		}
		if trakHandlerType(sr, atom) == handlerSound {
			return atom
		}
	}
	return first
}

// trakHandlerType returns the trak's mdia/hdlr handler type, or "" if absent.
func trakHandlerType(sr *binary.SafeReader, trak *Atom) string {
	mdia, err := findAtom(sr, trak.DataOffset(), trak.DataOffset()+trak.DataSize(), "mdia")
	if err != nil {
		return ""
	}
	hdlr, err := findAtom(sr, mdia.DataOffset(), mdia.DataOffset()+mdia.DataSize(), "hdlr")
	if err != nil || hdlr.DataSize() < 12 {
		return ""
	}
	// hdlr: version/flags(4) pre_defined(4) handler_type(4)
	buf := make([]byte, 4)
	if err := sr.ReadAt(buf, hdlr.DataOffset()+8, "hdlr handler type"); err != nil {
		return ""
	}
	return string(buf)
}

// parseMvhd parses the movie header atom for duration.
func parseMvhd(sr *binary.SafeReader, mvhdAtom *Atom, file *types.File) error {
	offset := mvhdAtom.DataOffset()

	// Read version (1 byte)
	version, err := sr.Read[uint8](offset, "mvhd version")
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
		file.Audio.Duration = mvhdDuration(duration, timescale)
	}

	return nil
}

// parseMvhdVersion0 parses 32-bit mvhd (version 0).
func parseMvhdVersion0(sr *binary.SafeReader, offset int64) (timescale uint32, duration uint64, err error) {
	// Skip creation time (4 bytes) and modification time (4 bytes)
	offset += 8

	// Read timescale (4 bytes)
	timescale, err = sr.Read[uint32](offset, "mvhd timescale")
	if err != nil {
		return 0, 0, err
	}
	offset += 4

	// Read duration (4 bytes)
	duration32, err := sr.Read[uint32](offset, "mvhd duration")
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
	timescale, err = sr.Read[uint32](offset, "mvhd timescale")
	if err != nil {
		return 0, 0, err
	}
	offset += 4

	// Read duration (8 bytes)
	duration, err = sr.Read[uint64](offset, "mvhd duration")
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
	numEntries, err := sr.Read[uint32](offset, "stsd entry count")
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

	entrySize, err := sr.Read[uint32](offset, "stsd entry size")
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
	_ = parseCodecDetails(sr, offset-8, entrySize, codec, file)

	return parseSoundSampleEntry(sr, offset-8, file)
}

// parseSoundSampleEntry reads channels and sample rate from an audio sample
// entry starting at entry (the offset of its size field). Versions 0 and 1 share
// the legacy layout (v1's extra fields follow the sample rate); version 2
// stores a float64 rate and a uint32 channel count further in. Other versions
// are left unset rather than guessed.
func parseSoundSampleEntry(sr *binary.SafeReader, entry int64, file *types.File) error {
	// size(4) format(4) reserved(6) data_reference_index(2) = 16 bytes, then:
	version, err := sr.Read[uint16](entry+16, "sound sample entry version")
	if err != nil {
		return err
	}

	switch version {
	case 0, 1:
		// version(2) revision(2) vendor(4), then channels(2) samplesize(2) compid(2) packetsize(2) rate(16.16)
		channels, err := sr.Read[uint16](entry+24, "channels")
		if err != nil {
			return err
		}
		file.Audio.Channels = int(channels)

		rate, err := sr.Read[uint32](entry+32, "sample rate")
		if err != nil {
			return err
		}
		file.Audio.SampleRate = int(rate >> 16) // integer part of 16.16 fixed point
	case 2:
		// Legacy fields hold constants; real values: float64 rate at +40, uint32 channels at +48.
		bits, err := sr.Read[uint64](entry+40, "sample rate")
		if err != nil {
			return err
		}
		channels, err := sr.Read[uint32](entry+48, "channels")
		if err != nil {
			return err
		}
		rate := math.Float64frombits(bits)
		if rate > 0 && rate <= math.MaxInt32 {
			file.Audio.SampleRate = int(rate)
		}
		file.Audio.Channels = int(min(channels, math.MaxUint16))
	}
	return nil
}

// mvhdDuration converts a duration in timescale units to a time.Duration without
// overflowing the intermediate product. Values beyond what time.Duration can
// hold are clamped to the maximum.
func mvhdDuration(duration uint64, timescale uint32) time.Duration {
	if timescale == 0 {
		return 0
	}
	ts := uint64(timescale)
	secs := duration / ts
	rem := duration % ts // rem < 2^32, so rem*1e9 < 2^62
	if secs > math.MaxInt64/uint64(time.Second) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(secs)*time.Second + time.Duration(rem*uint64(time.Second)/ts) //nolint:gosec // G115: rem < ts, so the quotient is < 1e9
}
