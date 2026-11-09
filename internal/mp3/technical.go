package mp3

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// MPEG audio version bits, (header >> 19) & 3. 1 is reserved.
const (
	versionMPEG25   = 0
	versionReserved = 1
	versionMPEG2    = 2
	versionMPEG1    = 3
)

// Layer III bitrate tables in kbps, indexed by the header's bitrate index.
// MPEG-2.5 uses the MPEG-2 table.
var (
	bitrateTableV1 = []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	bitrateTableV2 = []int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
)

// Sample rate tables in Hz, indexed by the header's sample rate index.
var (
	sampleRateTableV1  = []int{44100, 48000, 32000, 0}
	sampleRateTableV2  = []int{22050, 24000, 16000, 0}
	sampleRateTableV25 = []int{11025, 12000, 8000, 0}
)

// sameStreamMask selects the header bits every frame of one stream shares:
// sync, version, layer and sample rate index.
const sameStreamMask = 0xFFFE0C00

// mpegVersion extracts the version bits: 3 = MPEG-1, 2 = MPEG-2, 0 = MPEG-2.5.
func mpegVersion(header uint32) uint32 { return (header >> 19) & 0x3 }

// samplesPerFrame returns the Layer III samples per frame for the header's version.
func samplesPerFrame(header uint32) int {
	if mpegVersion(header) == versionMPEG1 {
		return 1152
	}
	return 576
}

// maxFrameSearch bounds how far past the ID3 tag the first frame is searched
// for. Real frame data begins within a few bytes of the tag; the margin covers
// padding and slightly wrong tag sizes without letting a crafted file force a
// scan of the whole input.
const maxFrameSearch = 1 << 16 // 64 KiB

var errNoMP3Frame = errors.New("no valid MP3 frame found")

// parseTechnicalInfo extracts bitrate, sample rate, codec, and duration from MP3 frames.
//
// It reads one bounded window starting at the end of the ID3 tag and scans it
// in memory for the first frame header that the next frame header confirms.
// fileSize is where the audio ends (before any ID3v1 trailer).
func parseTechnicalInfo(ctx context.Context, sr *binutil.SafeReader, tagSize int64, fileSize int64, file *types.File) error {
	start := max(tagSize, 0)
	if start >= fileSize {
		return errNoMP3Frame
	}

	window := make([]byte, min(fileSize-start, maxFrameSearch))
	if err := sr.ReadAt(window, start, "MP3 frame search window"); err != nil {
		return fmt.Errorf("read frame search window: %w", err)
	}

	for i := 0; i+4 <= len(window); i++ {
		if err := binutil.Canceled(ctx, i); err != nil {
			return err
		}

		header := binary.BigEndian.Uint32(window[i : i+4])
		if !validFrameHeader(header) {
			continue
		}
		bitrate, sampleRate, channels := parseMP3FrameHeader(header)
		if bitrate <= 0 || sampleRate <= 0 {
			continue
		}

		frameOffset := start + int64(i)
		next, hasNext, err := headerAt(sr, window, start, frameOffset+frameLength(header, bitrate, sampleRate), fileSize)
		if err != nil {
			return err
		}
		if hasNext && !sameStream(header, next) {
			continue // a false sync: the frame it describes is not followed by another
		}

		file.Audio.SampleRate = sampleRate
		file.Audio.Channels = channels
		file.Audio.Codec = "MP3"
		vbr := parseVBRHeader(sr, frameOffset, header, fileSize)
		file.Audio.VBR = vbr.kind == vbrXing || vbr.kind == vbrVBRI
		if vbr.frames == 0 {
			// No frame count: constant bitrate, duration from the audio size.
			file.Audio.Bitrate = bitrate
			file.Audio.Duration = estimateCBRDuration(bitrate, fileSize, tagSize)
			return nil
		}
		samples := int64(vbr.frames) * int64(samplesPerFrame(header))
		played := samples
		if vbr.gapless > 0 && vbr.gapless < samples {
			played -= vbr.gapless
		}
		file.Audio.Duration = samplesDuration(played, sampleRate)
		file.Audio.Bitrate = streamBitrate(vbr, samples, sampleRate, fileSize-frameOffset, bitrate, next, hasNext)
		return nil
	}

	return errNoMP3Frame
}

// headerAt returns the 4 bytes at off as a frame header, from window (which
// starts at windowStart) when they are inside it. ok is false when they would
// run past end.
func headerAt(sr *binutil.SafeReader, window []byte, windowStart, off, end int64) (header uint32, ok bool, err error) {
	if off+4 > end {
		return 0, false, nil
	}
	if rel := off - windowStart; rel+4 <= int64(len(window)) {
		return binary.BigEndian.Uint32(window[rel : rel+4]), true, nil
	}
	header, err = sr.Read[uint32](off, "next MP3 frame header")
	if err != nil {
		return 0, false, fmt.Errorf("read next frame header: %w", err)
	}
	return header, true, nil
}

// sameStream reports whether next is a valid frame header of the stream that
// header starts.
func sameStream(header, next uint32) bool {
	if !validFrameHeader(next) || next&sameStreamMask != header&sameStreamMask {
		return false
	}
	bitrate, _, _ := parseMP3FrameHeader(next)
	return bitrate > 0
}

// streamBitrate returns the bitrate of a stream whose first frame carries a
// frame count: the bitrate of the frame after an Info header (the Info frame
// itself may use another), else the stream's bytes over its duration. The
// Xing/VBRI byte count is used when present, else audioBytes.
func streamBitrate(vbr vbrHeader, samples int64, sampleRate int, audioBytes int64, firstBitrate int, next uint32, hasNext bool) int {
	if vbr.kind == vbrInfo {
		if hasNext {
			if b, _, _ := parseMP3FrameHeader(next); b > 0 {
				return b
			}
		}
		return firstBitrate
	}
	n := audioBytes
	if vbr.bytes > 0 {
		n = int64(vbr.bytes)
	}
	return int(n * 8 * int64(sampleRate) / samples)
}

// validFrameHeader reports whether header has the 11-bit frame sync and
// names an MPEG-1, MPEG-2 or MPEG-2.5 Layer III frame.
func validFrameHeader(header uint32) bool {
	if header&0xFFE00000 != 0xFFE00000 {
		return false
	}
	if mpegVersion(header) == versionReserved {
		return false
	}
	return (header>>17)&0x3 == 1 // Layer III
}

// parseMP3FrameHeader extracts bitrate, sample rate, and channels from frame header.
func parseMP3FrameHeader(header uint32) (bitrate, sampleRate, channels int) {
	bitrates, sampleRates := bitrateTableV1, sampleRateTableV1
	switch mpegVersion(header) {
	case versionMPEG2:
		bitrates, sampleRates = bitrateTableV2, sampleRateTableV2
	case versionMPEG25:
		bitrates, sampleRates = bitrateTableV2, sampleRateTableV25
	}

	// Bitrate index (4 bits)
	bitrateIdx := (header >> 12) & 0xF
	if bitrateIdx < uint32(len(bitrates)) { //nolint:gosec // G115: len of a small constant table
		bitrate = bitrates[bitrateIdx] * 1000 // Convert to bps
	}

	// Sample rate index (2 bits)
	sampleRateIdx := (header >> 10) & 0x3
	if sampleRateIdx < uint32(len(sampleRates)) { //nolint:gosec // G115: len of a small constant table
		sampleRate = sampleRates[sampleRateIdx]
	}

	// Channel mode (2 bits)
	channelMode := (header >> 6) & 0x3
	if channelMode == 3 {
		channels = 1 // Mono
	} else {
		channels = 2 // Stereo, Joint Stereo, Dual Channel
	}

	return
}

// frameLength returns the length in bytes of the Layer III frame that header
// starts, padding included: 144 (MPEG-1) or 72 (MPEG-2 and 2.5) times
// bitrate over sample rate, plus the padding byte.
func frameLength(header uint32, bitrate, sampleRate int) int64 {
	coef := 144
	if mpegVersion(header) != versionMPEG1 {
		coef = 72
	}
	return int64(coef*bitrate/sampleRate) + int64(header>>9&1)
}

// sideInfoSize returns the Layer III side-information size in bytes for the
// given frame header: MPEG-1: 32 (stereo modes) / 17 (mono); MPEG-2 and 2.5: 17 / 9.
func sideInfoSize(header uint32) int64 {
	mono := (header>>6)&0x3 == 3
	if mpegVersion(header) == versionMPEG1 {
		if mono {
			return 17
		}
		return 32
	}
	if mono {
		return 9
	}
	return 17
}

// vbrKind is the kind of header found in a stream's first frame.
type vbrKind uint8

const (
	vbrNone vbrKind = iota
	vbrXing         // "Xing": a VBR stream
	vbrInfo         // "Info": a CBR stream with the Xing layout (LAME, ffmpeg)
	vbrVBRI         // "VBRI": Fraunhofer's VBR header
)

// vbrHeader is what a Xing, Info or VBRI header says about the stream.
type vbrHeader struct {
	kind    vbrKind
	frames  uint32 // audio frames; 0 when not given
	bytes   uint32 // audio bytes; 0 when not given
	gapless int64  // encoder delay plus padding, in samples, from a LAME tag; 0 when absent
}

// Xing header flags.
const (
	xingFrames  = 0x1
	xingBytes   = 0x2
	xingTOC     = 0x4
	xingQuality = 0x8
)

// xingReadLen covers the Xing header with every optional field (120 bytes)
// and the LAME tag up to its delay and padding (24 bytes).
const xingReadLen = 120 + 24

// parseVBRHeader reads the Xing/Info header (and a LAME tag after it) or the
// VBRI header of the first frame. end bounds the read.
func parseVBRHeader(sr *binutil.SafeReader, frameOffset int64, header uint32, end int64) vbrHeader {
	// Xing/Info follows the frame header and the side information, whose size
	// depends on MPEG version and channel mode.
	xingOffset := frameOffset + 4 + sideInfoSize(header)
	if n := min(xingReadLen, end-xingOffset); n >= 8 {
		buf := make([]byte, n)
		if err := sr.ReadAt(buf, xingOffset, "VBR header"); err == nil {
			switch string(buf[:4]) {
			case "Xing":
				return parseXing(vbrXing, buf)
			case "Info":
				return parseXing(vbrInfo, buf)
			}
		}
	}
	// VBRI is always 32 bytes after the 4-byte header, regardless of channel mode.
	var vbri [18]byte
	if err := sr.ReadAt(vbri[:], frameOffset+36, "VBRI header"); err == nil && string(vbri[:4]) == "VBRI" {
		return vbrHeader{
			kind:   vbrVBRI,
			bytes:  binary.BigEndian.Uint32(vbri[10:14]),
			frames: binary.BigEndian.Uint32(vbri[14:18]),
		}
	}
	return vbrHeader{}
}

// parseXing decodes a Xing/Info header (buf starts at its marker) and the
// encoder delay and padding of a LAME tag after it.
func parseXing(kind vbrKind, buf []byte) vbrHeader {
	h := vbrHeader{kind: kind}
	flags := binary.BigEndian.Uint32(buf[4:8])
	p := 8
	field := func(flag uint32) uint32 {
		if flags&flag == 0 || p+4 > len(buf) {
			return 0
		}
		v := binary.BigEndian.Uint32(buf[p : p+4])
		p += 4
		return v
	}
	h.frames = field(xingFrames)
	h.bytes = field(xingBytes)
	if flags&xingTOC != 0 {
		p += 100
	}
	if flags&xingQuality != 0 {
		p += 4
	}
	// LAME tag: a 9-byte encoder version, then delay (12 bits) and padding
	// (12 bits) at bytes 21-23. LAME and ffmpeg (Lavc/Lavf) write it.
	if p+24 <= len(buf) {
		switch string(buf[p : p+4]) {
		case "LAME", "Lavc", "Lavf":
			d := buf[p+21 : p+24]
			delay := int64(d[0])<<4 | int64(d[1])>>4
			padding := int64(d[1]&0x0F)<<8 | int64(d[2])
			h.gapless = delay + padding
		}
	}
	return h
}

// samplesDuration returns the duration of n samples at rate Hz, exactly.
func samplesDuration(n int64, rate int) time.Duration {
	r := int64(rate)
	return time.Duration(n/r)*time.Second + time.Duration(n%r)*time.Second/time.Duration(r)
}

// estimateCBRDuration estimates duration for constant bitrate files.
func estimateCBRDuration(bitrate int, fileSize int64, tagSize int64) time.Duration {
	if bitrate == 0 {
		return 0
	}

	// Audio data size (excluding ID3 tag)
	audioSize := fileSize - tagSize

	// Duration = (audio size in bytes * 8 bits/byte) / bitrate
	durationSeconds := float64(audioSize*8) / float64(bitrate)
	return time.Duration(durationSeconds * float64(time.Second))
}
