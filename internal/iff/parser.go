package iff

import (
	"context"
	"fmt"
	"io"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/mp3"
	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/replaygain"
	"github.com/simonhull/audiometa/internal/types"
)

// Native comment keys, the fallback for an ID3 tag without COMM.
const (
	keyICMT = "ICMT"
	keyANNO = "ANNO"
)

// parser implements registry.FormatParser and registry.ArtworkExtractor for
// one of the two formats.
type parser struct {
	format types.Format
}

// open walks the container and checks that it holds the parser's format.
func (p *parser) open(ctx context.Context, r io.ReaderAt, size int64, path string) (*container, error) {
	sr := binutil.NewSafeReader(r, size, path)
	c, err := walk(ctx, sr, size)
	if err != nil {
		return nil, err
	}
	wav := c.formType == formWAVE && c.magic != magicFORM
	aiff := (c.formType == formAIFF || c.formType == formAIFC) && c.magic == magicFORM
	if (p.format == types.FormatWAV && !wav) || (p.format == types.FormatAIFF && !aiff) {
		return nil, corrupted(sr, 0, fmt.Sprintf("%s %s is not a %s file", c.magic, c.formType, p.format))
	}
	return c, nil
}

// id3 returns a reader over the payload of the ID3v2 chunk, and its size.
// WAV files use "id3 " or "ID3 ", AIFF files "ID3 ".
func (c *container) id3(r io.ReaderAt) (*io.SectionReader, bool) {
	ch, ok := c.first(idID3WAV, idID3AIFF)
	if !ok {
		return nil, false
	}
	n := c.present(ch)
	return io.NewSectionReader(r, ch.data(), n), true
}

// text returns the native text tags and the key of the native comment.
func (c *container) text() ([]entry, string, error) {
	if c.magic == magicFORM {
		entries, err := c.readAIFFText()
		return entries, keyANNO, err
	}
	i := c.infoIndex()
	if i < 0 {
		return nil, keyICMT, nil
	}
	entries, err := c.readInfo(c.chunks[i])
	return entries, keyICMT, err
}

// Parse reads the audio properties and the tags.
func (p *parser) Parse(ctx context.Context, r io.ReaderAt, size int64, path string) (*types.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := p.open(ctx, r, size, path)
	if err != nil {
		return nil, err
	}
	entries, commentID, err := c.text()
	if err != nil {
		return nil, err
	}
	file := &types.File{Path: path, Format: p.format, Size: size, Warnings: c.warnings}

	audio := c.parseFmt
	if p.format == types.FormatAIFF {
		audio = c.parseCOMM
	}
	if err := audio(file); err != nil {
		return nil, err
	}
	if tag, ok := c.id3(r); ok {
		if err := mp3.ReadEmbeddedTag(ctx, tag, tag.Size(), path, file); err != nil {
			return nil, err
		}
	}
	addRaw(file, entries, commentID)
	types.BindFormat(&file.Tags, p.format)
	replaygain.Apply(file)
	return file, nil
}

// ExtractArtwork returns the pictures of the ID3v2 chunk. The native text
// tags hold none.
func (p *parser) ExtractArtwork(ctx context.Context, r io.ReaderAt, size int64, path string) ([]types.Artwork, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := p.open(ctx, r, size, path)
	if err != nil {
		return nil, err
	}
	tag, ok := c.id3(r)
	if !ok {
		return nil, nil
	}
	return mp3.EmbeddedArtwork(ctx, tag, tag.Size(), path)
}

func init() {
	for _, f := range []types.Format{types.FormatWAV, types.FormatAIFF} {
		registry.Register(f, &parser{format: f})
		registry.RegisterWriter(f, &writer{format: f})
	}
}
