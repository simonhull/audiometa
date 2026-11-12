package audiometa_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/simonhull/audiometa"
)

func TestCanWrite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		format audiometa.Format
		want   bool
	}{
		{audiometa.FormatUnknown, false},
		{audiometa.FormatFLAC, true},
		{audiometa.FormatMP3, true},
		{audiometa.FormatM4A, true},
		{audiometa.FormatM4B, true},
		{audiometa.FormatOgg, false},
		{audiometa.FormatOpus, false},
		{audiometa.FormatWAV, true},
		{audiometa.FormatAIFF, true},
		{audiometa.Format(250), false},
	}
	for _, tt := range tests {
		t.Run(tt.format.String(), func(t *testing.T) {
			t.Parallel()
			if got := audiometa.CanWrite(tt.format); got != tt.want {
				t.Errorf("CanWrite(%v) = %v, want %v", tt.format, got, tt.want)
			}
		})
	}
}

// TestCanWrite_AgreesWithWriteTo pins CanWrite to what WriteTo does: a format
// it denies is refused with ErrUnsupportedWrite, and one it allows is not.
func TestCanWrite_AgreesWithWriteTo(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"testdata/sample.flac", "testdata/sample.mp3", "testdata/sample.m4a", "testdata/sample.wav", "testdata/sample.aiff", "testdata/sample.ogg", "testdata/sample.opus"} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			f := openFile(t, path)
			f.Tags.Title = "Changed"
			_, err := f.WriteTo(&bytes.Buffer{})
			if refused := errors.Is(err, audiometa.ErrUnsupportedWrite); refused == audiometa.CanWrite(f.Format) {
				t.Errorf("CanWrite(%v) = %v, WriteTo err = %v", f.Format, audiometa.CanWrite(f.Format), err)
			}
		})
	}
}

// TestWritable_CorpusFiles is the contract of the dry run: every writable
// corpus file reaches its first output byte, so Writable returns nil.
func TestWritable_CorpusFiles(t *testing.T) {
	t.Parallel()
	paths, err := filepath.Glob("testdata/sample*")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			f := openFile(t, path)
			err := f.Writable()
			if !audiometa.CanWrite(f.Format) {
				if !errors.Is(err, audiometa.ErrUnsupportedWrite) {
					t.Errorf("Writable = %v, want ErrUnsupportedWrite", err)
				}
				return
			}
			if err != nil {
				t.Errorf("Writable = %v, want nil", err)
			}
		})
	}
}

func TestWritable_RefusalsOfTheFile(t *testing.T) {
	t.Parallel()
	m4a := readTestdata(t, "testdata/sample.m4a")
	wantDRM := bytes.ReplaceAll(m4a, []byte("mp4a"), []byte("enca"))
	if bytes.Equal(wantDRM, m4a) {
		t.Fatal("sample.m4a has no mp4a sample entry to turn into an encrypted one")
	}
	fragmented := append(bytes.Clone(m4a), 0, 0, 0, 8, 'm', 'o', 'o', 'f')

	tests := []struct {
		name string
		path string
		want error
	}{
		{"DRM-protected M4A", tempFileOf(t, "drm.m4a", wantDRM), audiometa.ErrDRMProtected},
		{"fragmented MP4", tempFileOf(t, "fragmented.m4a", fragmented), audiometa.ErrUnsupportedWrite},
		{"Ogg Vorbis has no writer", "testdata/sample.ogg", audiometa.ErrUnsupportedWrite},
		{"Opus has no writer", "testdata/sample.opus", audiometa.ErrUnsupportedWrite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := openFile(t, tt.path)
			if err := f.Writable(); !errors.Is(err, tt.want) {
				t.Errorf("Writable = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestWritable_AgreesWithWriteTo checks that a refusal Writable reports is the
// one WriteTo gives for the same file.
func TestWritable_AgreesWithWriteTo(t *testing.T) {
	t.Parallel()
	m4a := readTestdata(t, "testdata/sample.m4a")
	f := openFile(t, tempFileOf(t, "drm.m4a", bytes.ReplaceAll(m4a, []byte("mp4a"), []byte("enca"))))
	f.Tags.Title = "Changed"
	_, writeErr := f.WriteTo(&bytes.Buffer{})
	if !errors.Is(writeErr, audiometa.ErrDRMProtected) || !errors.Is(f.Writable(), audiometa.ErrDRMProtected) {
		t.Errorf("WriteTo = %v, Writable = %v; both want ErrDRMProtected", writeErr, f.Writable())
	}
}

func TestWritable_IgnoresPendingEditsAndLeavesFileUnchanged(t *testing.T) {
	t.Parallel()
	path := tempFileOf(t, "w.mp3", readTestdata(t, "testdata/sample.mp3"))
	before := readTestdata(t, path)
	f := openFile(t, path)
	f.Tags.Title = "Pending"
	if err := f.Writable(); err != nil {
		t.Fatal(err)
	}
	if f.Tags.Title != "Pending" {
		t.Errorf("Title = %q after Writable, want the pending edit kept", f.Tags.Title)
	}
	if !bytes.Equal(readTestdata(t, path), before) {
		t.Error("Writable changed the file")
	}
}

func TestWritableContext_Canceled(t *testing.T) {
	t.Parallel()
	f := openFile(t, "testdata/sample.flac")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.WritableContext(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("WritableContext = %v, want context.Canceled", err)
	}
}

func TestWritable_AfterClose(t *testing.T) {
	t.Parallel()
	f, err := audiometa.Open("testdata/sample.flac")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Writable(); err == nil {
		t.Error("Writable after Close = nil, want the read error")
	}
}
