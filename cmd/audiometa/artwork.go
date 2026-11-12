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
// Permissions of what the artwork command creates: owner-only files, and directories
// the owner and group can read.
const (
	dirMode  = 0o750
	fileMode = 0o600
)

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
	if err := os.MkdirAll(*outDir, dirMode); err != nil {
		fmt.Fprintf(stderr, "audiometa artwork: %v\n", err)
		return exitFail
	}
	out := &printer{w: stdout}
	code := exitOK
	written := map[string]string{}
	for _, path := range fs.Args() {
		if err := extractArtwork(path, *outDir, *force, written, out); err != nil {
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

// extractArtwork writes the pictures of the file at path into dir. written
// maps the output names this run has used, case-folded, to the input that
// used them; a second input wanting one is refused, even with force.
func extractArtwork(path, dir string, force bool, written map[string]string, out *printer) (err error) {
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
		key := strings.ToLower(dest)
		if prev, ok := written[key]; ok {
			return fmt.Errorf("%s was already written from %s in this run; extract files with the same name into different directories", dest, prev)
		}
		if err := writeNew(dest, img.Data, force); err != nil {
			return err
		}
		written[key] = path
		out.printf("wrote %s (%s)\n", dest, cleanText(img.String()))
	}
	return nil
}

// writeNew writes data to dest, refusing to replace an existing file unless
// force is set. Without force, creation is exclusive, so nothing at dest is
// followed or replaced. With force, data goes to a temporary file in the same
// directory that is then renamed over dest: a symlink at dest is replaced, not
// followed, and a directory at dest makes the rename fail.
func writeNew(dest string, data []byte, force bool) error {
	if force {
		return replaceFile(dest, data)
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s exists (use --force to overwrite)", dest)
		}
		return err
	}
	_, err = f.Write(data)
	return errors.Join(err, f.Close())
}

// replaceFile writes data to a new file beside dest and renames it to dest.
func replaceFile(dest string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".audiometa-artwork-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(tmp.Name()))
		}
	}()
	_, err = tmp.Write(data)
	if err = errors.Join(err, tmp.Close()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}
