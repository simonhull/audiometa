package main

import (
	"encoding/json/v2"
	"fmt"
	"strings"
	"unicode"

	"github.com/simonhull/audiometa"
)

// schemaVersion is the version of the JSON contract emitted by every command.
// Adding fields is compatible; renaming a field or changing a unit requires a
// bump.
const schemaVersion = 1

// The DTOs below are the CLI's JSON contract. They are deliberately separate
// from the library types so the output cannot change when internals do.

type fileDTO struct {
	SchemaVersion int          `json:"schema_version"`
	Path          string       `json:"path"`
	Error         string       `json:"error,omitempty"`
	Format        string       `json:"format,omitempty"`
	Size          int64        `json:"size,omitzero"`
	Tags          *tagsDTO     `json:"tags,omitzero"`
	Audio         *audioDTO    `json:"audio,omitzero"`
	Chapters      []chapterDTO `json:"chapters"`
	Warnings      []warningDTO `json:"warnings"`
}

type chaptersDTO struct {
	SchemaVersion int          `json:"schema_version"`
	Path          string       `json:"path"`
	Error         string       `json:"error,omitempty"`
	Chapters      []chapterDTO `json:"chapters"`
}

type chapterDTO struct {
	Index   int    `json:"index"`
	Title   string `json:"title"`
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
	Source  string `json:"source,omitzero"`
}

type warningDTO struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	Offset  int64  `json:"offset,omitzero"`
	Error   string `json:"error,omitempty"`
}

type tagsDTO struct {
	Title               string              `json:"title,omitempty"`
	Subtitle            string              `json:"subtitle,omitempty"`
	Artist              string              `json:"artist,omitempty"`
	Artists             []string            `json:"artists,omitempty"`
	Album               string              `json:"album,omitempty"`
	AlbumArtist         string              `json:"album_artist,omitempty"`
	Narrator            string              `json:"narrator,omitempty"`
	Composers           []string            `json:"composers,omitempty"`
	Performers          []string            `json:"performers,omitempty"`
	Genres              []string            `json:"genres,omitempty"`
	Year                int                 `json:"year,omitzero"`
	Date                string              `json:"date,omitempty"`
	OriginalDate        string              `json:"original_date,omitempty"`
	TrackNumber         int                 `json:"track_number,omitzero"`
	TrackTotal          int                 `json:"track_total,omitzero"`
	DiscNumber          int                 `json:"disc_number,omitzero"`
	DiscTotal           int                 `json:"disc_total,omitzero"`
	Series              string              `json:"series,omitempty"`
	SeriesPart          string              `json:"series_part,omitempty"`
	Grouping            string              `json:"grouping,omitempty"`
	Comment             string              `json:"comment,omitempty"`
	Description         string              `json:"description,omitempty"`
	Lyrics              string              `json:"lyrics,omitempty"`
	Publisher           string              `json:"publisher,omitempty"`
	Label               string              `json:"label,omitempty"`
	CatalogNumber       string              `json:"catalog_number,omitempty"`
	Barcode             string              `json:"barcode,omitempty"`
	Copyright           string              `json:"copyright,omitempty"`
	ISBN                string              `json:"isbn,omitempty"`
	ASIN                string              `json:"asin,omitempty"`
	ISRC                string              `json:"isrc,omitempty"`
	Language            string              `json:"language,omitempty"`
	MusicBrainzTrackID  string              `json:"musicbrainz_track_id,omitempty"`
	MusicBrainzAlbumID  string              `json:"musicbrainz_album_id,omitempty"`
	MusicBrainzArtistID string              `json:"musicbrainz_artist_id,omitempty"`
	Raw                 map[string][]string `json:"raw,omitempty"`
}

type audioDTO struct {
	Codec            string         `json:"codec,omitempty"`
	CodecDescription string         `json:"codec_description,omitempty"`
	CodecProfile     string         `json:"codec_profile,omitempty"`
	Container        string         `json:"container,omitempty"`
	DurationMs       int64          `json:"duration_ms"`
	SampleRate       int            `json:"sample_rate,omitzero"`
	BitDepth         int            `json:"bit_depth,omitzero"`
	Channels         int            `json:"channels,omitzero"`
	Bitrate          int            `json:"bitrate,omitzero"`
	Lossless         bool           `json:"lossless"`
	VBR              bool           `json:"vbr"`
	ReplayGain       *replayGainDTO `json:"replay_gain,omitzero"`
}

type replayGainDTO struct {
	TrackGainDB float64 `json:"track_gain_db"`
	TrackPeak   float64 `json:"track_peak"`
	AlbumGainDB float64 `json:"album_gain_db"`
	AlbumPeak   float64 `json:"album_peak"`
}

// validUTF8 replaces invalid UTF-8 with U+FFFD. Tag text comes from untrusted
// files and encoding/json/v2 rejects invalid UTF-8, so every string that
// reaches the DTOs goes through here.
func validUTF8(s string) string { return strings.ToValidUTF8(s, "�") }

func validUTF8s(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = validUTF8(s)
	}
	return out
}

// toFileDTO converts an opened file into its JSON form.
func toFileDTO(f *audiometa.File) fileDTO {
	d := fileDTO{
		SchemaVersion: schemaVersion,
		Path:          validUTF8(f.Path),
		Format:        f.Format.String(),
		Size:          f.Size,
		Tags:          toTagsDTO(&f.Tags),
		Audio:         toAudioDTO(&f.Audio),
		Chapters:      toChapterDTOs(f),
		Warnings:      make([]warningDTO, 0, len(f.Warnings)),
	}
	for _, w := range f.Warnings {
		wd := warningDTO{Stage: validUTF8(w.Stage), Message: validUTF8(w.Message), Offset: w.Offset}
		if w.Err != nil {
			wd.Error = validUTF8(w.Err.Error())
		}
		d.Warnings = append(d.Warnings, wd)
	}
	return d
}

// toErrorDTO is the record emitted for a file that could not be opened.
func toErrorDTO(path string, err error) fileDTO {
	return fileDTO{
		SchemaVersion: schemaVersion,
		Path:          validUTF8(path),
		Error:         validUTF8(err.Error()),
		Chapters:      []chapterDTO{},
		Warnings:      []warningDTO{},
	}
}

func toChapterDTOs(f *audiometa.File) []chapterDTO {
	out := make([]chapterDTO, 0, len(f.Chapters))
	for _, c := range f.Chapters {
		out = append(out, chapterDTO{
			Index:   c.Index,
			Title:   validUTF8(c.Title),
			StartMs: c.StartTime.Milliseconds(),
			EndMs:   c.EndTime.Milliseconds(),
			Source:  string(c.Source),
		})
	}
	return out
}

func toAudioDTO(a *audiometa.AudioInfo) *audioDTO {
	d := &audioDTO{
		Codec:            validUTF8(a.Codec),
		CodecDescription: validUTF8(a.CodecDescription),
		CodecProfile:     validUTF8(a.CodecProfile),
		Container:        validUTF8(a.Container),
		DurationMs:       a.Duration.Milliseconds(),
		SampleRate:       a.SampleRate,
		BitDepth:         a.BitDepth,
		Channels:         a.Channels,
		Bitrate:          a.Bitrate,
		Lossless:         a.Lossless,
		VBR:              a.VBR,
	}
	if rg := a.ReplayGain; rg != nil {
		d.ReplayGain = &replayGainDTO{
			TrackGainDB: rg.TrackGain,
			TrackPeak:   rg.TrackPeak,
			AlbumGainDB: rg.AlbumGain,
			AlbumPeak:   rg.AlbumPeak,
		}
	}
	return d
}

func toTagsDTO(t *audiometa.Tags) *tagsDTO {
	d := &tagsDTO{
		Title:               validUTF8(t.Title),
		Subtitle:            validUTF8(t.Subtitle),
		Artist:              validUTF8(t.Artist),
		Artists:             validUTF8s(t.Artists),
		Album:               validUTF8(t.Album),
		AlbumArtist:         validUTF8(t.AlbumArtist),
		Narrator:            validUTF8(t.Narrator),
		Composers:           validUTF8s(t.Composers),
		Performers:          validUTF8s(t.Performers),
		Genres:              validUTF8s(t.Genres),
		Year:                t.Year,
		Date:                validUTF8(t.Date),
		OriginalDate:        validUTF8(t.OriginalDate),
		TrackNumber:         t.TrackNumber,
		TrackTotal:          t.TrackTotal,
		DiscNumber:          t.DiscNumber,
		DiscTotal:           t.DiscTotal,
		Series:              validUTF8(t.Series),
		SeriesPart:          validUTF8(t.SeriesPart),
		Grouping:            validUTF8(t.Grouping),
		Comment:             validUTF8(t.Comment),
		Description:         validUTF8(t.Description),
		Lyrics:              validUTF8(t.Lyrics),
		Publisher:           validUTF8(t.Publisher),
		Label:               validUTF8(t.Label),
		CatalogNumber:       validUTF8(t.CatalogNumber),
		Barcode:             validUTF8(t.Barcode),
		Copyright:           validUTF8(t.Copyright),
		ISBN:                validUTF8(t.ISBN),
		ASIN:                validUTF8(t.ASIN),
		ISRC:                validUTF8(t.ISRC),
		Language:            validUTF8(t.Language),
		MusicBrainzTrackID:  validUTF8(t.MusicBrainzTrackID),
		MusicBrainzAlbumID:  validUTF8(t.MusicBrainzAlbumID),
		MusicBrainzArtistID: validUTF8(t.MusicBrainzArtistID),
	}
	for key, values := range t.All() {
		if d.Raw == nil {
			d.Raw = make(map[string][]string)
		}
		// Distinct invalid keys can collapse to one; merge rather than drop.
		k := validUTF8(key)
		d.Raw[k] = append(d.Raw[k], validUTF8s(values)...)
	}
	return d
}

// marshal encodes v with deterministic (sorted) map keys.
func marshal(v any) ([]byte, error) {
	return json.Marshal(v, json.Deterministic(true))
}

// writeJSONLine writes v as a single JSON line.
func writeJSONLine(p *printer, v any) error {
	b, err := marshal(v)
	if err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	p.write(append(b, '\n'))
	return p.err
}

// cleanText makes untrusted text safe for a terminal: invalid UTF-8 and
// control characters (including escape sequences and newlines) are replaced.
func cleanText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, validUTF8(s))
}
