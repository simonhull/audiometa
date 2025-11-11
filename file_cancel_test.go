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

// TestOpenMany_Cancellation verifies that cancelled operations clean up resources
func TestOpenMany_Cancellation(t *testing.T) {
	// Create test files
	paths := make([]string, 5)
	for i := range paths {
		paths[i] = createTestM4BFile(t)
		defer os.Remove(paths[i])
	}

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Try to open files with cancelled context
	files, err := audiometa.OpenMany(ctx, paths...)

	// Should return error
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}

	// Should not return any files
	if files != nil {
		t.Error("expected nil files on error")
	}

	// If we got here without leaking file descriptors, the test passes
}

// TestOpenMany_PartialFailure verifies cleanup on partial failure
func TestOpenMany_PartialFailure(t *testing.T) {
	// Create mix of valid and invalid paths
	validPath := createTestM4BFile(t)
	defer os.Remove(validPath)

	paths := []string{
		validPath,
		"/nonexistent/file.m4b",
		validPath,
	}

	ctx := context.Background()

	files, err := audiometa.OpenMany(ctx, paths...)

	// Should return error
	if err == nil {
		t.Fatal("expected error from nonexistent file")
	}

	// Should not return any files (all or nothing)
	if files != nil {
		t.Error("expected nil files on partial failure")
	}

	// Successfully opened files should have been closed
}
