package iff

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/simonhull/audiometa/internal/types"
)

// WAVE format tags of the fmt chunk.
const (
	wavePCM        = 0x0001
	waveFloat      = 0x0003
	waveALaw       = 0x0006
	waveMuLaw      = 0x0007
	waveMPEG       = 0x0050
	waveMP3        = 0x0055
	waveExtensible = 0xFFFE
)

// Codec names.
const (
	codecPCM   = "PCM"
	codecFloat = "IEEE float"
	codecALaw  = "A-law"
	codecMuLaw = "mu-law"
)

// Sizes of the audio chunks' fixed parts.
const (
	fmtMinSize        = 16
	fmtExtensibleSize = 40
	commSize          = 18
	aifcCommSize      = 22
	ssndHeaderSize    = 8 // offset and block size
	extendedSize      = 10
)

// Limits of the values audio properties are reported in. A rate is an int,
// which is 32 bits on some platforms; a duration is a time.Duration.
const (
	maxRate    = math.MaxInt32
	maxSeconds = int64(math.MaxInt64 / time.Second)
)

// sampleFormat is what a fmt or COMM chunk says about the audio.
type sampleFormat struct {
	codec      string
	lossless   bool
	channels   int
	sampleRate int64
	bits       int
	frameSize  int // bytes per sample frame, when constant
}

// parseFmt reads the WAV fmt chunk and the data size into file.Audio.
func (c *container) parseFmt(file *types.File) error {
	sf, tag, byteRate, err := c.readFmt()
	if err != nil {
		return err
	}
	file.Audio.Container = "WAV"
	if c.magic == magicRF64 {
		file.Audio.Container = magicRF64
	}
	data, ok := c.first(idData)
	if !ok {
		technical(file, "WAV file without a data chunk")
		sf.apply(file, 0, 0)
		return nil
	}
	bytes := c.present(data)
	var frames int64
	switch {
	case sf.frameSize > 0 && (sf.lossless || tag == waveALaw || tag == waveMuLaw):
		frames = bytes / int64(sf.frameSize)
	case !sf.lossless:
		frames = c.factFrames()
	}
	sf.apply(file, frames, bytes)
	if frames == 0 && byteRate > 0 && file.Audio.SampleRate > 0 {
		// A compressed format without a fact chunk: its average byte rate is
		// all there is.
		if secs := bytes / byteRate; secs >= maxSeconds {
			technical(file, fmt.Sprintf("duration of %d bytes at %d bytes per second is out of range", bytes, byteRate))
			return nil
		}
		file.Audio.Duration = time.Duration(float64(bytes) / float64(byteRate) * float64(time.Second))
		setBitrate(file, byteRate, 8)
	}
	return nil
}

// readFmt reads the fmt chunk: the sample format, its format tag (the
// sub-format of WAVE_FORMAT_EXTENSIBLE) and the average byte rate.
func (c *container) readFmt() (sampleFormat, uint16, int64, error) {
	ch, ok := c.first("fmt ")
	if !ok {
		return sampleFormat{}, 0, 0, corrupted(c.sr, headerSize, "WAV file without a fmt chunk")
	}
	if ch.size < fmtMinSize || c.present(ch) < fmtMinSize {
		return sampleFormat{}, 0, 0, corrupted(c.sr, ch.off, fmt.Sprintf("fmt chunk of %d bytes is too short", ch.size))
	}
	b := make([]byte, min(c.present(ch), fmtExtensibleSize))
	if err := c.sr.ReadAt(b, ch.data(), "fmt chunk"); err != nil {
		return sampleFormat{}, 0, 0, err
	}
	tag := c.order.Uint16(b[0:])
	sf := sampleFormat{
		channels:   int(c.order.Uint16(b[2:])),
		sampleRate: int64(c.order.Uint32(b[4:])),
		frameSize:  int(c.order.Uint16(b[12:])),
		bits:       int(c.order.Uint16(b[14:])),
	}
	if tag == waveExtensible && len(b) == fmtExtensibleSize {
		if valid := int(c.order.Uint16(b[18:])); valid > 0 && valid <= sf.bits {
			sf.bits = valid
		}
		tag = c.order.Uint16(b[24:]) // The sub-format GUID starts with the format tag.
	}
	sf.codec, sf.lossless = waveCodec(tag)
	return sf, tag, int64(c.order.Uint32(b[8:])), nil
}

// factFrames returns the sample count of the fact chunk, which compressed WAV
// files carry, or 0.
func (c *container) factFrames() int64 {
	ch, ok := c.first("fact")
	if !ok || c.present(ch) < 4 {
		return 0
	}
	var b [4]byte
	if err := c.sr.ReadAt(b[:], ch.data(), "fact chunk"); err != nil {
		return 0
	}
	return int64(c.order.Uint32(b[:]))
}

// waveCodec names a WAVE format tag and says whether it is lossless.
func waveCodec(tag uint16) (string, bool) {
	switch tag {
	case wavePCM:
		return codecPCM, true
	case waveFloat:
		return codecFloat, true
	case waveALaw:
		return codecALaw, false
	case waveMuLaw:
		return codecMuLaw, false
	case waveMPEG:
		return "MPEG", false
	case waveMP3:
		return "MP3", false
	}
	return fmt.Sprintf("WAVE format 0x%04X", tag), false
}

// parseCOMM reads the AIFF COMM chunk and the SSND size into file.Audio.
func (c *container) parseCOMM(file *types.File) error {
	ch, ok := c.first("COMM")
	if !ok {
		return corrupted(c.sr, headerSize, "AIFF file without a COMM chunk")
	}
	want := int64(commSize)
	if c.formType == formAIFC {
		want = aifcCommSize
	}
	if ch.size < want || c.present(ch) < want {
		return corrupted(c.sr, ch.off, fmt.Sprintf("COMM chunk of %d bytes is too short", ch.size))
	}
	b := make([]byte, want)
	if err := c.sr.ReadAt(b, ch.data(), "COMM chunk"); err != nil {
		return err
	}
	sf := sampleFormat{
		channels: int(binary.BigEndian.Uint16(b[0:])),
		bits:     int(binary.BigEndian.Uint16(b[6:])),
	}
	frames := int64(binary.BigEndian.Uint32(b[2:]))
	rate := extendedFloat([extendedSize]byte(b[8:18]))
	if rate > 0 && rate < math.MaxInt32 {
		sf.sampleRate = int64(math.Round(rate))
	}
	file.Audio.Container = "AIFF"
	sf.codec, sf.lossless = codecPCM, true
	if c.formType == formAIFC {
		file.Audio.Container = "AIFF-C"
		sf.codec, sf.lossless = aifcCodec(string(b[18:22]))
	}

	var bytes int64
	if ssnd, ok := c.first(idSSND); ok {
		bytes = max(c.present(ssnd)-ssndHeaderSize, 0)
	} else if frames > 0 {
		technical(file, "AIFF file without an SSND chunk")
	}
	sf.apply(file, frames, bytes)
	return nil
}

// aifcCodec names an AIFF-C compression type and says whether it is lossless.
func aifcCodec(compression string) (string, bool) {
	switch compression {
	case "NONE", "twos", "sowt", "raw ", "in24", "in32":
		return codecPCM, true
	case "fl32", "FL32", "fl64", "FL64":
		return codecFloat, true
	case "alaw", "ALAW":
		return codecALaw, false
	case "ulaw", "ULAW":
		return codecMuLaw, false
	}
	return compression, false
}

// extendedFloat decodes an IEEE 754 80-bit extended float, the form AIFF
// gives its sample rate in.
func extendedFloat(b [extendedSize]byte) float64 {
	se := binary.BigEndian.Uint16(b[0:])
	mantissa := binary.BigEndian.Uint64(b[2:])
	exp := int(se & 0x7FFF)
	if exp == 0x7FFF {
		return math.NaN()
	}
	v := math.Ldexp(float64(mantissa), exp-16383-63)
	if se&0x8000 != 0 {
		v = -v
	}
	return v
}

// apply sets file.Audio from the sample format, frames of audio and the bytes
// they take. A value too large to report is left at 0 with a StageTechnical
// warning.
func (sf sampleFormat) apply(file *types.File, frames, bytes int64) {
	a := &file.Audio
	a.Codec, a.Lossless = sf.codec, sf.lossless
	a.Channels, a.BitDepth = sf.channels, sf.bits
	if !sf.lossless {
		a.BitDepth = 0 // A compressed codec's bits per sample are not its depth.
	}
	if sf.sampleRate > maxRate {
		technical(file, fmt.Sprintf("sample rate of %d Hz is out of range", sf.sampleRate))
		return
	}
	a.SampleRate = int(sf.sampleRate)
	if sf.sampleRate <= 0 || frames <= 0 {
		return
	}
	rate := sf.sampleRate
	if frames/rate >= maxSeconds {
		technical(file, fmt.Sprintf("duration of %d frames at %d Hz is out of range", frames, rate))
		return
	}
	a.Duration = time.Duration(frames/rate)*time.Second + time.Duration(frames%rate)*time.Second/time.Duration(rate)
	if sf.lossless {
		setBitrate(file, rate, int64(sf.channels), int64(sf.bits))
	} else if secs := a.Duration.Seconds(); secs > 0 {
		if bps := float64(bytes) * 8 / secs; bps <= maxRate {
			a.Bitrate = int(bps)
		} else {
			technical(file, fmt.Sprintf("bit rate of %.0f bits per second is out of range", bps))
		}
	}
}

// setBitrate sets file.Audio.Bitrate to the product of factors, or warns and
// leaves it 0 when the product is past maxRate.
func setBitrate(file *types.File, factors ...int64) {
	r := int64(1)
	for _, f := range factors {
		if f > 0 && r > maxRate/f {
			technical(file, "bit rate is out of range")
			return
		}
		r *= f
	}
	file.Audio.Bitrate = int(r)
}

// technical records a StageTechnical warning.
func technical(file *types.File, msg string) {
	file.Warnings = types.AppendWarning(file.Warnings, types.Warning{Stage: types.StageTechnical, Message: msg})
}
