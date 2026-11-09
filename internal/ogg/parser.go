package ogg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/replaygain"
	"github.com/simonhull/audiometa/internal/types"
)

const containerOgg = "Ogg"

// parser implements the audiometa.FormatParser interface for Ogg Vorbis and Opus files.
type parser struct{}

// Parse parses an Ogg Vorbis or Opus file and extracts metadata.
func (p *parser) Parse(ctx context.Context, r io.ReaderAt, size int64, path string) (*types.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Create safe reader
	sr := binary.NewSafeReader(r, size, path)

	// Verify Ogg magic bytes ("OggS")
	// A direct read: filling a SafeReader window here would cost 16 KiB that
	// the page stream, which reads the same bytes next, does not share.
	var magic [4]byte
	if size < int64(len(magic)) {
		return nil, fmt.Errorf("read Ogg magic: %w", &binary.OutOfBoundsError{Path: path, What: "Ogg magic bytes", Offset: 0, Length: len(magic), Size: size})
	}
	if n, err := r.ReadAt(magic[:], 0); n < len(magic) && err != nil {
		return nil, fmt.Errorf("read Ogg magic: %s: failed to read Ogg magic bytes at offset 0: %w", path, err)
	}
	if string(magic[:]) != capturePattern {
		return nil, &types.CorruptedFileError{
			Path:   path,
			Offset: 0,
			Reason: "invalid Ogg magic bytes",
		}
	}

	// Initialize file
	file := &types.File{
		Path:   path,
		Format: types.FormatOgg,
		Size:   size,
		Tags:   types.Tags{},
		Audio:  types.AudioInfo{},
	}

	h, err := readHeaderPackets(ctx, r, size, path, file, maxHeaderBytes)
	if err != nil {
		return nil, err
	}
	packets := h.packets

	if len(packets) < 2 {
		return nil, fmt.Errorf("not enough packets found (need at least 2, got %d)", len(packets))
	}

	c := detectCodec(packets[0])
	if c == nil {
		return nil, errUnknownCodec
	}
	file.Format = c.format

	if err := c.parseID(packets[0], file); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", c.idName, err)
	}

	// Duration first: the comment parser derives the last chapter's end time from it.
	// It comes from the last granule position and the sample rate the identification header gave.
	if file.Audio.SampleRate > 0 {
		duration, err := calculateDuration(ctx, sr, size, h, file.Audio.SampleRate)
		if err != nil {
			// Non-fatal - add warning
			types.Warn(file, types.StageTechnical, 0, err, fmt.Sprintf("failed to calculate duration: %v", err))
		} else {
			file.Audio.Duration = duration
		}
	}

	// An unset nominal bitrate (Opus has none) leaves the estimate from the audio pages.
	if file.Audio.Bitrate == 0 {
		file.Audio.Bitrate = types.AverageBitrate(audioBytes(h, size), file.Audio.Duration)
	}

	if err := c.parseComments(ctx, packets[1], file); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// Non-fatal - add warning
		types.Warn(file, types.StageMetadata, 0, err, fmt.Sprintf("failed to parse %s: %v", c.commentName, err))
	}

	replaygain.Apply(file)

	return file, nil
}

// maxHeaderBytes bounds how much page data is read while looking for the
// identification and comment headers. Comment packets carrying embedded cover
// art (METADATA_BLOCK_PICTURE) can span many pages. Every scanned page counts
// its full size, header included, whichever stream it belongs to.
const maxHeaderBytes = 16 << 20

// maxHeaderPages bounds how many pages are scanned while looking for the
// headers. A page costs at least 27 bytes of file, so without it a run of
// empty pages would cost memory out of proportion to the bytes it charges.
// A comment header with a 16 MiB cover spans a few thousand pages at most.
const maxHeaderPages = 1 << 16

// headers is what readHeaderPackets found at the start of the first logical stream.
type headers struct {
	packets    [][]byte
	serial     uint32
	audioStart int64 // offset of the page after the one ending the last header packet; 0 if not reached
}

// readHeaderPackets reads pages from the start of the first logical stream until
// its header packets (three for Vorbis, two for Opus; two for an unrecognized
// codec) are complete, the file ends, or maxBytes of pages or maxHeaderPages
// pages have been scanned. A read error on the first page is fatal; later
// errors and hitting the cap are recorded as warnings and parsing continues
// with the packets gathered so far.
//
// The first audio packet starts a new page (Vorbis I §A.2, RFC 7845 §3), so
// audioStart is where the audio pages begin.
func readHeaderPackets(ctx context.Context, r io.ReaderAt, size int64, path string, file *types.File, maxBytes int64) (headers, error) {
	stream := newPageStream(r, size, path, false)
	var pages []*oggPage
	var serial uint32
	var read int64
	var packets packetTracker
	complete := 0 // packets ended so far
	want := 2     // header packets expected; the first page names the codec
	var audioStart int64
	offset := int64(0)

	for i := 0; offset < size && complete < want; i++ {
		if err := binary.Canceled(ctx, i); err != nil {
			return headers{}, err
		}

		page, nextOffset, err := stream.next()
		if err != nil {
			if i == 0 {
				return headers{}, fmt.Errorf("failed to read first Ogg page: %w", err)
			}
			types.Warn(file, types.StageMetadata, offset, err, fmt.Sprintf("failed to read Ogg page %d: %v", i, err))
			break
		}
		read += nextOffset - offset
		offset = nextOffset
		if read > maxBytes {
			types.Warn(file, types.StageMetadata, offset, nil, fmt.Sprintf("Ogg header search exceeds %d bytes; truncated", maxBytes))
			break
		}
		if i >= maxHeaderPages {
			types.Warn(file, types.StageMetadata, offset, nil, fmt.Sprintf("Ogg header search exceeds %d pages; truncated", maxHeaderPages))
			break
		}
		if i == 0 {
			serial = page.SerialNumber
			if c := detectCodec(page.Data); c != nil {
				want = c.headerPackets
			}
		} else if page.SerialNumber != serial {
			continue // another multiplexed stream
		}

		pages = append(pages, page)
		for seg := range packets.segments(page) {
			if seg.ends {
				complete++
				if complete == want {
					audioStart = offset // offset is already the end of this page
				}
			}
		}
	}

	if len(pages) == 0 {
		return headers{}, errors.New("no Ogg pages found")
	}
	return headers{packets: extractPackets(pages), serial: serial, audioStart: audioStart}, nil
}

// audioBytes estimates the bytes of audio pages: everything after the
// header pages, page headers included. It is 0 when the headers never ended.
func audioBytes(h headers, size int64) int64 {
	if h.audioStart <= 0 || h.audioStart > size {
		return 0
	}
	return size - h.audioStart
}

// calculateDuration returns the duration of the first logical stream, h.serial:
// its last granule position divided by sampleRate.
func calculateDuration(ctx context.Context, sr *binary.SafeReader, fileSize int64, h headers, sampleRate int) (time.Duration, error) {
	if sampleRate == 0 {
		return 0, errors.New("sample rate is zero")
	}

	// Find last page's granule position
	granule, err := lastGranule(ctx, sr, fileSize, h)
	if err != nil {
		return 0, err
	}

	// Granule position -1 means "not set"
	if granule < 0 {
		return 0, errors.New("granule position not set")
	}

	// Calculate duration (granule is in samples)
	seconds := float64(granule) / float64(sampleRate)
	return time.Duration(seconds * float64(time.Second)), nil
}

// init registers the Ogg parser for both Vorbis and Opus formats.
func init() {
	p := &parser{}
	registry.Register(types.FormatOgg, p)
	registry.Register(types.FormatOpus, p)
}
