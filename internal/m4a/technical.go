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
	mvhdAtom, err := moovAtom.child(ctx, sr, "mvhd")
	if err != nil {
		return nil
	}

	// Parse mvhd for duration
	if err := parseMvhd(sr, mvhdAtom, file); err != nil {
		return nil
	}

	// Find the audio trak for format info
	// Path: moov -> trak (handler type "soun")
	trakAtom := selectAudioTrak(ctx, sr, moovAtom)
	if trakAtom == nil {
		return nil
	}

	// Find mdia atom
	// Path: trak -> mdia
	mdiaAtom, err := trakAtom.child(ctx, sr, "mdia")
	if err != nil {
		return nil
	}

	// Find minf atom
	// Path: mdia -> minf
	minfAtom, err := mdiaAtom.child(ctx, sr, "minf")
	if err != nil {
		return nil
	}

	// Find stbl atom
	// Path: minf -> stbl
	stblAtom, err := minfAtom.child(ctx, sr, "stbl")
	if err != nil {
		return nil
	}

	// Find stsd (sample description) atom
	// Path: stbl -> stsd
	stsdAtom, err := stblAtom.child(ctx, sr, "stsd")
	if err != nil {
		return nil
	}

	// Parse stsd for codec, sample rate, channels
	source, err := parseStsd(ctx, sr, stsdAtom, file)
	if err != nil {
		return nil
	}

	// The legacy 16.16 field cannot hold rates above 65535 Hz (writers store
	// the timescale's low bits or 0 there), but the track's media header does.
	if source == rateLegacyField {
		applyMediaTimescale(ctx, sr, mdiaAtom, file)
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
	stsz, err := stbl.child(ctx, sr, "stsz")
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
func selectAudioTrak(ctx context.Context, sr *binary.SafeReader, moovAtom *Atom) *Atom {
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
		if trakHandlerType(ctx, sr, atom) == handlerSound {
			return atom
		}
	}
	return first
}

// trakHandlerType returns the trak's mdia/hdlr handler type, or "" if absent.
func trakHandlerType(ctx context.Context, sr *binary.SafeReader, trak *Atom) string {
	mdia, err := trak.child(ctx, sr, "mdia")
	if err != nil {
		return ""
	}
	hdlr, err := mdia.child(ctx, sr, "hdlr")
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

// rateSource says where file.Audio.SampleRate came from.
type rateSource uint8

const (
	// rateUnknown means no sample rate was read.
	rateUnknown rateSource = iota
	// rateLegacyField is the 16.16 field of a version 0 or 1 sample entry,
	// which cannot represent rates above 65535 Hz.
	rateLegacyField
	// rateDeclared is a rate the file states in full: a version 2 sample
	// entry's float64 or a codec configuration box.
	rateDeclared
)

// mdhdTimescale returns the timescale of mdia's media header, or false when
// the mdhd atom is absent or unreadable.
func mdhdTimescale(ctx context.Context, sr *binary.SafeReader, mdia *Atom) (uint32, bool) {
	mdhd, err := mdia.child(ctx, sr, "mdhd")
	if err != nil {
		return 0, false
	}
	version, err := sr.Read[uint8](mdhd.DataOffset(), "mdhd version")
	if err != nil {
		return 0, false
	}
	// version/flags(4), then creation and modification times (4 bytes each in
	// version 0, 8 in version 1), then the timescale.
	var offset int64
	switch version {
	case 0:
		offset = 12
	case 1:
		offset = 20
	default:
		return 0, false
	}
	timescale, err := sr.Read[uint32](mdhd.DataOffset()+offset, "mdhd timescale")
	if err != nil {
		return 0, false
	}
	return timescale, true
}

// applyMediaTimescale replaces a sample rate read from the 16.16 field with
// the media timescale when that field cannot have held the real rate: the
// timescale is above 65535, or the field was 0.
func applyMediaTimescale(ctx context.Context, sr *binary.SafeReader, mdia *Atom, file *types.File) {
	timescale, ok := mdhdTimescale(ctx, sr, mdia)
	if !ok || (timescale <= math.MaxUint16 && (file.Audio.SampleRate != 0 || timescale == 0)) {
		return
	}
	file.Audio.SampleRate = int(min(timescale, math.MaxInt32))
}

// parseStsd parses the sample description atom for codec, sample rate, channels.
// The result says where the sample rate came from.
func parseStsd(ctx context.Context, sr *binary.SafeReader, stsdAtom *Atom, file *types.File) (rateSource, error) {
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
		return rateUnknown, err
	}
	offset += 4

	if numEntries == 0 {
		return rateUnknown, nil
	}

	// Read first entry (audio sample description)
	// Entry structure:
	// [4 bytes] size
	// [4 bytes] format (codec type)
	// ... more fields ...

	entrySize, err := sr.Read[uint32](offset, "stsd entry size")
	if err != nil {
		return rateUnknown, err
	}
	offset += 4

	// Read format (codec type) - 4 bytes
	formatBytes := make([]byte, 4)
	if err := sr.ReadAt(formatBytes, offset, "stsd format"); err != nil {
		return rateUnknown, err
	}
	offset += 4

	codec := string(formatBytes)
	file.Audio.Codec = codec
	file.Audio.Lossless = codec == codecALAC || codec == codecFLAC

	// Parse enhanced codec details (non-fatal if it fails)
	entry := offset - 8 // the offset of the entry's size field
	_ = parseCodecDetails(sr, entry, entrySize, codec, file)

	source, err := parseSoundSampleEntry(sr, entry, file)
	if err != nil {
		return rateUnknown, err
	}
	return applyLosslessConfig(ctx, sr, entry, entrySize, codec, file, source), nil
}

// losslessConfig holds the stream properties a lossless codec's configuration
// box declares. A zero field is absent or failed validation.
type losslessConfig struct {
	sampleRate int
	bitDepth   int
	channels   int
}

// valid reports which fields of c are plausible, zeroing the others.
func (c losslessConfig) valid() losslessConfig {
	if c.sampleRate < 1 || c.sampleRate > math.MaxInt32 {
		c.sampleRate = 0
	}
	if c.bitDepth < 4 || c.bitDepth > 32 {
		c.bitDepth = 0
	}
	if c.channels < 1 || c.channels > 8 {
		c.channels = 0
	}
	return c
}

// soundEntryChildren returns where the child boxes of an audio sample entry
// start, relative to the entry: after the 36-byte version 0 layout and the
// 16 (version 1) or 36 (version 2) bytes that later versions add. ok is false
// for versions this package does not know.
func soundEntryChildren(version uint16) (offset int64, ok bool) {
	switch version {
	case 0:
		return 36, true
	case 1:
		return 52, true
	case 2:
		return 72, true
	}
	return 0, false
}

// applyLosslessConfig overrides the sample entry's channels and sample rate
// with those of the codec's own configuration box, and sets the bit depth,
// for ALAC and FLAC. entry is the offset of the sample entry's size field. It
// is best effort: whatever is missing or implausible leaves file untouched.
// It returns source, or rateDeclared when the configuration supplied the rate.
func applyLosslessConfig(ctx context.Context, sr *binary.SafeReader, entry int64, entrySize uint32, codec string, file *types.File, source rateSource) rateSource {
	var boxType string
	var decode func(*binary.SafeReader, *Atom) (losslessConfig, bool)
	switch codec {
	case codecALAC:
		boxType, decode = "alac", alacConfig
	case codecFLAC:
		boxType, decode = "dfLa", flacConfig
	default:
		return source
	}

	version, err := sr.Read[uint16](entry+16, "sound sample entry version")
	if err != nil {
		return source
	}
	children, ok := soundEntryChildren(version)
	if !ok || int64(entrySize) < children {
		return source
	}
	box, err := findAtom(ctx, sr, entry+children, entry+int64(entrySize), boxType)
	if err != nil {
		return source
	}
	cfg, ok := decode(sr, box)
	if !ok {
		return source
	}

	cfg = cfg.valid()
	if cfg.sampleRate != 0 {
		file.Audio.SampleRate = cfg.sampleRate
		source = rateDeclared
	}
	if cfg.bitDepth != 0 {
		file.Audio.BitDepth = cfg.bitDepth
	}
	if cfg.channels != 0 {
		file.Audio.Channels = cfg.channels
	}
	return source
}

// alacConfig decodes the ALACSpecificConfig held by an "alac" child box. The
// box is a FullBox in files ffmpeg and iTunes write; the version and flags are
// absent in some, so the config is located by the box's size.
func alacConfig(sr *binary.SafeReader, box *Atom) (losslessConfig, bool) {
	const configSize = 24
	start := box.DataOffset()
	switch size := box.DataSize(); {
	case size == configSize:
	case size >= configSize+4:
		start += 4
	default:
		return losslessConfig{}, false
	}
	var cfg [configSize]byte
	if err := sr.ReadAt(cfg[:], start, "ALAC magic cookie"); err != nil {
		return losslessConfig{}, false
	}
	// frameLength(4) compatibleVersion(1) bitDepth(1) pb(1) mb(1) kb(1)
	// numChannels(1) maxRun(2) maxFrameBytes(4) avgBitRate(4) sampleRate(4)
	return losslessConfig{
		bitDepth:   int(cfg[5]),
		channels:   int(cfg[9]),
		sampleRate: int(uint32(cfg[20])<<24 | uint32(cfg[21])<<16 | uint32(cfg[22])<<8 | uint32(cfg[23])),
	}, true
}

// flacConfig decodes the STREAMINFO block that opens the "dfLa" child box of a
// FLAC sample entry: a FullBox, then FLAC metadata blocks.
func flacConfig(sr *binary.SafeReader, box *Atom) (losslessConfig, bool) {
	const streamInfoSize = 34
	if box.DataSize() < 4+4+streamInfoSize {
		return losslessConfig{}, false
	}
	var buf [4 + streamInfoSize]byte
	if err := sr.ReadAt(buf[:], box.DataOffset()+4, "dfLa STREAMINFO"); err != nil {
		return losslessConfig{}, false
	}
	// Block header: last-block flag and type (1 byte), length (3 bytes).
	length := int(buf[1])<<16 | int(buf[2])<<8 | int(buf[3])
	if buf[0]&0x7F != 0 || length != streamInfoSize {
		return losslessConfig{}, false
	}
	// STREAMINFO bytes 10-13: sample rate (20 bits), channels-1 (3 bits),
	// bits per sample-1 (5 bits), then the high bits of the sample count.
	si := buf[4:]
	return losslessConfig{
		sampleRate: int(si[10])<<12 | int(si[11])<<4 | int(si[12])>>4,
		channels:   int(si[12]>>1&0x07) + 1,
		bitDepth:   int(si[12]&1)<<4 | int(si[13]>>4) + 1,
	}, true
}

// parseSoundSampleEntry reads channels and sample rate from an audio sample
// entry starting at entry (the offset of its size field). Versions 0 and 1 share
// the legacy layout (v1's extra fields follow the sample rate); version 2
// stores a float64 rate and a uint32 channel count further in. Other versions
// are left unset rather than guessed. The result says where the rate came from.
func parseSoundSampleEntry(sr *binary.SafeReader, entry int64, file *types.File) (rateSource, error) {
	// size(4) format(4) reserved(6) data_reference_index(2) = 16 bytes, then:
	version, err := sr.Read[uint16](entry+16, "sound sample entry version")
	if err != nil {
		return rateUnknown, err
	}

	switch version {
	case 0, 1:
		// version(2) revision(2) vendor(4), then channels(2) samplesize(2) compid(2) packetsize(2) rate(16.16)
		channels, err := sr.Read[uint16](entry+24, "channels")
		if err != nil {
			return rateUnknown, err
		}
		file.Audio.Channels = int(channels)

		rate, err := sr.Read[uint32](entry+32, "sample rate")
		if err != nil {
			return rateUnknown, err
		}
		file.Audio.SampleRate = int(rate >> 16) // integer part of 16.16 fixed point
		return rateLegacyField, nil
	case 2:
		// Legacy fields hold constants; real values: float64 rate at +40, uint32 channels at +48.
		bits, err := sr.Read[uint64](entry+40, "sample rate")
		if err != nil {
			return rateUnknown, err
		}
		channels, err := sr.Read[uint32](entry+48, "channels")
		if err != nil {
			return rateUnknown, err
		}
		file.Audio.Channels = int(min(channels, math.MaxUint16))
		if rate := math.Float64frombits(bits); rate > 0 && rate <= math.MaxInt32 {
			file.Audio.SampleRate = int(rate)
			return rateDeclared, nil
		}
	}
	return rateUnknown, nil
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
