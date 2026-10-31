package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// Exit codes.
const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

const usageText = `audiometa - read audio file metadata (FLAC, MP3, M4A/M4B, Ogg Vorbis, Opus)

Usage:
  audiometa [inspect] [--json] FILE...     show format, audio properties, tags, chapters, warnings
  audiometa chapters [--json] FILE...      list chapter markers
  audiometa artwork --out DIR [--force] FILE...
                                           extract embedded artwork into DIR
  audiometa atoms FILE...                  dump the MP4/M4A/M4B atom tree (debugging)
  audiometa version [--json]               print version information
  audiometa help                           show this help

A bare "audiometa FILE" is the same as "audiometa inspect FILE". Flags must
come before the file names.

JSON output is NDJSON: one object per file, in argument order, so it streams
over any number of files and can be piped straight into jq. Every object has a
"schema_version" field; durations are integer milliseconds in fields ending in
"_ms". A file that cannot be read yields an object with an "error" field.

Exit codes:
  0  success
  1  at least one file failed to open or parse, or a write failed
  2  usage error (unknown flag or command, missing files or required flag)
`

// command is a subcommand entry point.
type command func(args []string, stdout, stderr io.Writer) int

func commands() map[string]command {
	return map[string]command{
		"inspect":  runInspect,
		"chapters": runChapters,
		"artwork":  runArtwork,
		"atoms":    runAtoms,
		"version":  runVersion,
	}
}

// run is the testable entry point: it dispatches args and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return exitUsage
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usageText)
		return exitOK
	case "--version", "-version":
		return runVersion(nil, stdout, stderr)
	}
	if cmd, ok := commands()[args[0]]; ok {
		return cmd(args[1:], stdout, stderr)
	}
	// Anything else (a path, or flags such as --json) is shorthand for inspect.
	return runInspect(args, stdout, stderr)
}

// newFlagSet builds a flag set that reports to stderr with a usage line.
func newFlagSet(name, synopsis string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("audiometa "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: audiometa %s %s\n", name, synopsis)
		fs.PrintDefaults()
	}
	return fs
}

// parse parses args. ok is false when the caller should return code.
func parse(fs *flag.FlagSet, args []string) (code int, ok bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK, false
		}
		return exitUsage, false
	}
	return exitOK, true
}

// requireFiles reports a usage error when no file arguments were given.
func requireFiles(fs *flag.FlagSet, stderr io.Writer) bool {
	if fs.NArg() > 0 {
		return true
	}
	fmt.Fprintf(stderr, "audiometa %s: no files given\n", strings.TrimPrefix(fs.Name(), "audiometa "))
	fs.Usage()
	return false
}

// printer writes to w and remembers the first write error.
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) printf(format string, args ...any) {
	if p.err != nil {
		return
	}
	_, p.err = fmt.Fprintf(p.w, format, args...)
}

func (p *printer) write(b []byte) {
	if p.err != nil {
		return
	}
	_, p.err = p.w.Write(b)
}
