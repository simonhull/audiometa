package audiometa

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// oneWarningFLAC is "fLaC" + a last-block STREAMINFO header of length 0: exactly one warning.
var oneWarningFLAC = []byte{'f', 'L', 'a', 'C', 0x80, 0, 0, 0}

func TestWarningPolicy(t *testing.T) {
	path := writeTemp(t, "a.flac", oneWarningFLAC)
	tests := []struct {
		name         string
		opts         []Option
		wantErr      bool
		wantWarnings int
	}{
		{"default", nil, false, 1},
		{"ignore", []Option{WithIgnoreWarnings()}, false, 0},
		{"strict", []Option{WithStrictParsing()}, true, 0},
		{"strict beats ignore", []Option{WithStrictParsing(), WithIgnoreWarnings()}, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := Open(path, tt.opts...)
			if tt.wantErr {
				if err == nil {
					_ = f.Close()
					t.Fatal("expected error")
				}
				if f != nil {
					t.Error("file should be nil on error")
				}
				if !strings.HasPrefix(err.Error(), "strict parsing failed: failed to parse STREAMINFO") {
					t.Errorf("unexpected message: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if len(f.Warnings) != tt.wantWarnings {
				t.Errorf("warnings = %d, want %d", len(f.Warnings), tt.wantWarnings)
			}
		})
	}
}

func TestApplyWarningPolicy_JoinsErrors(t *testing.T) {
	errA, errB := errors.New("a"), errors.New("b")
	file := &types.File{Warnings: []types.Warning{
		{Message: "first", Err: errA},
		{Message: "second", Err: errB},
	}}
	err := applyWarningPolicy(file, &openOptions{strictParsing: true})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, errA) || !errors.Is(err, errB) {
		t.Errorf("errors.Is should match both warning errors: %v", err)
	}
	if !strings.HasSuffix(err.Error(), "(and 1 more warnings)") {
		t.Errorf("unexpected message: %v", err)
	}
}

func TestWarningCap(t *testing.T) {
	data := append([]byte("fLaC"), make([]byte, 4000)...)
	f, err := Open(writeTemp(t, "flood.flac", data))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got := len(f.Warnings); got != types.MaxWarnings+1 {
		t.Fatalf("warnings = %d, want %d", got, types.MaxWarnings+1)
	}
	if last := f.Warnings[len(f.Warnings)-1]; !strings.Contains(last.Message, "suppressed") {
		t.Errorf("last warning = %q", last.Message)
	}
}

type panicParser struct{}

func (panicParser) Parse(context.Context, io.ReaderAt, int64, string) (*types.File, error) {
	panic("boom")
}

func (panicParser) ExtractArtwork(context.Context, io.ReaderAt, int64, string) ([]Artwork, error) {
	panic("boom")
}

func TestOpen_RecoversParserPanic(t *testing.T) {
	orig := registry.Get(types.FormatFLAC)
	registry.Register(types.FormatFLAC, panicParser{})
	t.Cleanup(func() { registry.Register(types.FormatFLAC, orig) })

	path := writeTemp(t, "a.flac", oneWarningFLAC)
	_, err := Open(path)
	var cfe *CorruptedFileError
	if !errors.As(err, &cfe) {
		t.Fatalf("want CorruptedFileError, got %v", err)
	}
	if !strings.Contains(cfe.Reason, "boom") {
		t.Errorf("reason = %q", cfe.Reason)
	}

	files, err := OpenMany(context.Background(), path, path)
	if err == nil {
		t.Fatal("expected joined error")
	}
	for i, f := range files {
		if f != nil {
			t.Errorf("files[%d] should be nil", i)
		}
	}
}

func TestExtractArtwork_RecoversPanic(t *testing.T) {
	f := &File{parser: panicParser{}, reader: bytes.NewReader(nil)}
	_, err := f.ExtractArtwork()
	var cfe *CorruptedFileError
	if !errors.As(err, &cfe) {
		t.Fatalf("want CorruptedFileError, got %v", err)
	}
}

type countingExtractor struct {
	panicParser
	calls atomic.Int32
	fail  bool
}

func (c *countingExtractor) ExtractArtwork(context.Context, io.ReaderAt, int64, string) ([]Artwork, error) {
	n := c.calls.Add(1)
	if c.fail {
		if n == 1 {
			return nil, errors.New("transient")
		}
		return []Artwork{{}}, nil
	}
	return nil, nil
}

func TestExtractArtwork_ConcurrentExtractOnce(t *testing.T) {
	ext := &countingExtractor{}
	f := &File{parser: ext, reader: bytes.NewReader(nil)}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			art, err := f.ExtractArtwork()
			if err != nil || art == nil || len(art) != 0 {
				t.Errorf("art=%v err=%v", art, err)
			}
		})
	}
	wg.Wait()
	if got := ext.calls.Load(); got != 1 {
		t.Errorf("extractor calls = %d, want 1", got)
	}
}

func TestExtractArtwork_ErrorNotCached(t *testing.T) {
	f := &File{parser: &countingExtractor{fail: true}, reader: bytes.NewReader(nil)}
	if _, err := f.ExtractArtwork(); err == nil {
		t.Fatal("first call should fail")
	}
	art, err := f.ExtractArtwork()
	if err != nil || len(art) != 1 {
		t.Fatalf("second call: art=%v err=%v", art, err)
	}
}

type noExtractor struct{}

func (noExtractor) Parse(context.Context, io.ReaderAt, int64, string) (*types.File, error) {
	return &types.File{}, nil
}

func TestExtractArtwork_NoExtractor(t *testing.T) {
	f := &File{parser: noExtractor{}, reader: bytes.NewReader(nil)}
	art, err := f.ExtractArtwork()
	if err != nil || art == nil || len(art) != 0 {
		t.Fatalf("art=%v err=%v", art, err)
	}
}
