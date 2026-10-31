package main

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/simonhull/audiometa"
)

// runInspect implements "audiometa inspect".
func runInspect(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("inspect", "[--json] FILE...", stderr)
	asJSON := fs.Bool("json", false, "emit one JSON object per file (NDJSON)")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if !requireFiles(fs, stderr) {
		return exitUsage
	}
	return eachFile(fs.Args(), stdout, stderr, *asJSON, func(f *audiometa.File, p *printer) error {
		if *asJSON {
			return writeJSONLine(p, toFileDTO(f))
		}
		writeInspectText(p, f)
		return p.err
	}, func(path string, err error, p *printer) error {
		return writeJSONLine(p, toErrorDTO(path, err))
	})
}

// eachFile opens every path, calls onFile for each success and, in JSON mode,
// onErr for each failure (otherwise failures go to stderr as "path: error").
// It returns the process exit code.
func eachFile(
	paths []string, stdout, stderr io.Writer, asJSON bool,
	onFile func(*audiometa.File, *printer) error,
	onErr func(string, error, *printer) error,
) int {
	out := &printer{w: stdout}
	code := exitOK
	for _, path := range paths {
		f, err := audiometa.Open(path)
		if err != nil {
			code = exitFail
			if asJSON {
				err = onErr(path, err, out)
			} else {
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				err = nil
			}
		} else {
			err = onFile(f, out)
			_ = f.Close() // read-only handle; nothing useful to do on failure
		}
		if err != nil {
			fmt.Fprintf(stderr, "audiometa: %v\n", err)
			return exitFail
		}
	}
	return code
}

type row struct{ label, value string }

func writeInspectText(p *printer, f *audiometa.File) {
	rows := []row{
		{"Path", f.Path},
		{"Format", f.Format.String()},
		{"Audio", f.Audio.String()},
		{"Duration", formatTime(f.Audio.Duration)},
	}
	rows = append(rows, tagRows(&f.Tags)...)
	rows = append(rows, row{"Chapters", strconv.Itoa(len(f.Chapters))})
	for _, r := range rows {
		if r.value != "" {
			p.printf("%-14s %s\n", r.label+":", cleanText(r.value))
		}
	}
	for _, w := range f.Warnings {
		p.printf("warning: %s\n", cleanText(w.String()))
	}
}

func tagRows(t *audiometa.Tags) []row {
	num := func(n int) string {
		if n == 0 {
			return ""
		}
		return strconv.Itoa(n)
	}
	of := func(n, total int) string {
		switch {
		case n == 0:
			return ""
		case total == 0:
			return strconv.Itoa(n)
		}
		return fmt.Sprintf("%d/%d", n, total)
	}
	return []row{
		{"Title", t.Title}, {"Subtitle", t.Subtitle},
		{"Artist", t.Artist}, {"Album", t.Album}, {"Album Artist", t.AlbumArtist},
		{"Narrator", t.Narrator}, {"Composers", strings.Join(t.Composers, ", ")},
		{"Genres", strings.Join(t.Genres, ", ")}, {"Year", num(t.Year)}, {"Date", t.Date},
		{"Track", of(t.TrackNumber, t.TrackTotal)}, {"Disc", of(t.DiscNumber, t.DiscTotal)},
		{"Series", t.Series}, {"Series Part", t.SeriesPart}, {"Grouping", t.Grouping},
		{"Publisher", t.Publisher}, {"Language", t.Language},
		{"ISBN", t.ISBN}, {"ASIN", t.ASIN}, {"Comment", t.Comment},
	}
}

// formatTime renders d as HH:MM:SS.mmm.
func formatTime(d time.Duration) string {
	ms := d.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, ms/60000%60, ms/1000%60, ms%1000)
}
