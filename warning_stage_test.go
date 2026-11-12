package audiometa

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestWarningStagesKnown opens the whole test corpus, whole and truncated to 60%, and
// requires every warning to carry one of the four published stages.
func TestWarningStagesKnown(t *testing.T) {
	t.Parallel()

	known := []string{StageMetadata, StageTechnical, StageChapters, StageArtwork}
	var paths []string
	for _, dir := range []string{"testdata", filepath.Join("testdata", "bench")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !e.IsDir() && !strings.HasSuffix(e.Name(), ".golden") {
				paths = append(paths, filepath.Join(dir, e.Name()))
			}
		}
	}

	damaged := t.TempDir()
	for _, p := range slices.Clone(paths) {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		cut := filepath.Join(damaged, filepath.Base(filepath.Dir(p))+"-"+filepath.Base(p))
		if err := os.WriteFile(cut, data[:len(data)*6/10], 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, cut)
	}

	warnings := 0
	for _, p := range paths {
		f, err := Open(p, WithArtworkPreload())
		if err != nil {
			continue // a damaged copy may not open at all
		}
		for _, w := range f.Warnings {
			warnings++
			if !slices.Contains(known, w.Stage) {
				t.Errorf("%s: warning %q has unknown stage %q", p, w.Message, w.Stage)
			}
		}
		_ = f.Close()
	}
	t.Logf("checked %d files, %d warnings", len(paths), warnings)
}
