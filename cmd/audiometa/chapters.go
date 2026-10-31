package main

import (
	"io"

	"github.com/simonhull/audiometa"
)

// runChapters implements "audiometa chapters".
func runChapters(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("chapters", "[--json] FILE...", stderr)
	asJSON := fs.Bool("json", false, "emit one JSON object per file (NDJSON)")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if !requireFiles(fs, stderr) {
		return exitUsage
	}
	return eachFile(fs.Args(), stdout, stderr, *asJSON, func(f *audiometa.File, p *printer) error {
		if *asJSON {
			return writeJSONLine(p, chaptersDTO{
				SchemaVersion: schemaVersion,
				Path:          validUTF8(f.Path),
				Chapters:      toChapterDTOs(f),
			})
		}
		for _, c := range f.Chapters {
			p.printf("[%d] %s → %s  %s\n", c.Index, formatTime(c.StartTime), formatTime(c.EndTime), cleanText(c.Title))
		}
		return p.err
	}, func(path string, err error, p *printer) error {
		return writeJSONLine(p, chaptersDTO{
			SchemaVersion: schemaVersion,
			Path:          validUTF8(path),
			Error:         validUTF8(err.Error()),
			Chapters:      []chapterDTO{},
		})
	})
}
