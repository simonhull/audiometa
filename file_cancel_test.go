package audiometa_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"testing"

	"github.com/simonhull/audiometa"
	_ "github.com/simonhull/audiometa/internal/flac"
	_ "github.com/simonhull/audiometa/internal/m4a"
	_ "github.com/simonhull/audiometa/internal/mp3"
	_ "github.com/simonhull/audiometa/internal/ogg"
)

func createTestM4BFile(t *testing.T) string {
	t.Helper()

	buf := &bytes.Buffer{}

	// ftyp atom
	ftypBuf := &bytes.Buffer{}
	ftypBuf.WriteString("M4B ")
	binary.Write(ftypBuf, binary.BigEndian, uint32(0))
	ftypBuf.WriteString("M4B ")

	ftypSize := uint32(8 + ftypBuf.Len())
	binary.Write(buf, binary.BigEndian, ftypSize)
	buf.WriteString("ftyp")
	buf.Write(ftypBuf.Bytes())

	// moov atom
	binary.Write(buf, binary.BigEndian, uint32(8))
	buf.WriteString("moov")

	tmpFile, err := os.CreateTemp("", "test*.m4b")
	if err != nil {
		t.Fatal(err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(buf.Bytes()); err != nil {
		t.Fatal(err)
	}

	return tmpFile.Name()
}

// TestOpenMany_Cancellation verifies that canceled operations report errors
// for each file slot via the joined error.
func TestOpenMany_Cancellation(t *testing.T) {
	// Create test files
	paths := make([]string, 5)
	for i := range paths {
		paths[i] = createTestM4BFile(t)
		defer os.Remove(paths[i])
	}

	// Create a context that's already canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	files, err := audiometa.OpenMany(ctx, paths...)

	if err == nil {
		t.Fatal("expected error from canceled context")
	}
	// Result slice is parallel to inputs; on cancellation every slot is nil.
	if len(files) != len(paths) {
		t.Fatalf("expected %d slots, got %d", len(paths), len(files))
	}
	for i, f := range files {
		if f != nil {
			t.Errorf("expected nil at slot %d on cancellation, got %v", i, f)
		}
	}
}

// TestOpenMany_PartialFailure verifies that successful files are returned
// alongside errors for the failed ones.
func TestOpenMany_PartialFailure(t *testing.T) {
	validPath := createTestM4BFile(t)
	defer os.Remove(validPath)

	paths := []string{
		validPath,
		"/nonexistent/file.m4b",
		validPath,
	}

	ctx := context.Background()

	files, err := audiometa.OpenMany(ctx, paths...)
	t.Cleanup(func() {
		for _, f := range files {
			if f != nil {
				_ = f.Close()
			}
		}
	})

	if err == nil {
		t.Fatal("expected error from nonexistent file")
	}
	if len(files) != len(paths) {
		t.Fatalf("expected %d slots, got %d", len(paths), len(files))
	}
	if files[0] == nil {
		t.Error("slot 0 (valid path) should not be nil")
	}
	if files[1] != nil {
		t.Error("slot 1 (nonexistent path) should be nil")
	}
	if files[2] == nil {
		t.Error("slot 2 (valid path) should not be nil")
	}
}
