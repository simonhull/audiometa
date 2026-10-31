package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/simonhull/audiometa"
)

// artworkExt maps an image MIME type to a file extension. Only these values
// are ever used: the extension is never derived from file contents.
func artworkExt(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	case "image/webp":
		return ".webp"
	case "image/tiff":
		return ".tiff"
	default:
		return ".bin"
	}
}

// artworkBase derives a safe output base name from an audio file path.
func artworkBase(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.Map(func(r rune) rune {
		if r < 0x20 || r == '/' || r == '\\' {
			return '_'
		}
		return r
	}, validUTF8(base))
	if base == "" || base == "." || base == ".." {
		return "artwork"
	}
	return base
}

// runArtwork implements "audiometa artwork".
func runArtwork(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("artwork", "--out DIR [--force] FILE...", stderr)
	outDir := fs.String("out", "", "directory to write images into (required)")
	force := fs.Bool("force", false, "overwrite existing files")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if *outDir == "" {
		fmt.Fprintln(stderr, "audiometa artwork: --out is required")
		fs.Usage()
		return exitUsage
	}
	if !requireFiles(fs, stderr) {
		return exitUsage
	}
	if err := os.MkdirAll(*outDir, 0o750); err != nil {
		fmt.Fprintf(stderr, "audiometa artwork: %v\n", err)
		return exitFail
	}
	out := &printer{w: stdout}
	code := exitOK
	for _, path := range fs.Args() {
		if err := extractArtwork(path, *outDir, *force, out); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			code = exitFail
		}
	}
	if out.err != nil {
		fmt.Fprintf(stderr, "audiometa artwork: %v\n", out.err)
		return exitFail
	}
	return code
}

func extractArtwork(path, dir string, force bool, out *printer) (err error) {
	f, err := audiometa.Open(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()

	images, err := f.ExtractArtwork()
	if err != nil {
		return err
	}
	base := artworkBase(path)
	for i, img := range images {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s_%d", base, i)
		}
		dest := filepath.Join(dir, name+artworkExt(img.MIMEType))
		if err := writeNew(dest, img.Data, force); err != nil {
			return err
		}
		out.printf("wrote %s (%s)\n", dest, cleanText(img.String()))
	}
	return nil
}

// writeNew writes data to dest, refusing to replace an existing file unless
// force is set. Creation is exclusive so an existing symlink is never followed.
func writeNew(dest string, data []byte, force bool) (err error) {
	flags := os.O_WRONLY | os.O_CREATE
	if force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(dest, flags, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s exists (use --force to overwrite)", dest)
		}
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	_, err = f.Write(data)
	return err
}
