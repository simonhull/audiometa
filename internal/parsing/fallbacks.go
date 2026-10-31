package parsing

import "github.com/simonhull/audiometa/internal/types"

// SeriesSource supplies the optional extra series-part candidates some formats
// consult between the Grouping fallback and the path fallback.
//
// The zero value yields the plain behavior used by FLAC, Ogg and MP3.
type SeriesSource struct {
	// ExplicitParts are explicit part values in priority order; the first
	// non-empty one wins (M4A: "Series Part", "Series Position", "Part", "Volume").
	ExplicitParts []string
	// Title and Album are parsed for a part number when set, in that order.
	Title, Album string
}

// ResolveSeriesFallbacks applies the shared post-parse series logic in place:
// Grouping fills Series (and SeriesPart if empty); then, when a series exists
// without a part, the part is taken from src's explicit values, its title, its
// album, and finally the file path, first match wins.
func ResolveSeriesFallbacks(tags *types.Tags, path string, src SeriesSource) {
	if tags.Series == "" && tags.Grouping != "" {
		if series, part := ParseGrouping(tags.Grouping); series != "" {
			tags.Series = series
			if tags.SeriesPart == "" && part != "" {
				tags.SeriesPart = part
			}
		}
	}
	if tags.Series == "" || tags.SeriesPart != "" {
		return
	}
	for _, part := range src.ExplicitParts {
		if part != "" {
			tags.SeriesPart = part
			return
		}
	}
	if part := ExtractSeriesPartFromText(src.Title); part != "" {
		tags.SeriesPart = part
		return
	}
	if part := ExtractSeriesPartFromText(src.Album); part != "" {
		tags.SeriesPart = part
		return
	}
	if part := ExtractSeriesPartFromPath(path); part != "" {
		tags.SeriesPart = part
	}
}

// ApplyNarratorFallback sets Narrator to the first Composer when no narrator
// was tagged. Only MP3 and M4A apply it; FLAC and Ogg deliberately do not.
func ApplyNarratorFallback(tags *types.Tags) {
	if tags.Narrator == "" && len(tags.Composers) > 0 {
		tags.Narrator = tags.Composers[0]
	}
}
