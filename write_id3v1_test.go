package audiometa_test

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/simonhull/audiometa"
)

const id3v1Len = 128

// readFileBytes returns the contents of path.
func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// trailerOf returns the ID3v1 trailer at the end of data, or nil when the last
// 128 bytes do not start with "TAG".
func trailerOf(data []byte) []byte {
	if len(data) < id3v1Len || string(data[len(data)-id3v1Len:][:3]) != "TAG" {
		return nil
	}
	return data[len(data)-id3v1Len:]
}

// trailerText returns the NUL-terminated text in trailer[from:to].
func trailerText(trailer []byte, from, to int) string {
	field := trailer[from:to]
	if i := bytes.IndexByte(field, 0); i >= 0 {
		field = field[:i]
	}
	return string(field)
}

// audioDigestOf returns the audio digest of the file at path.
func audioDigestOf(t *testing.T, path string) [32]byte {
	t.Helper()
	f := openFile(t, path)
	sum, err := f.AudioDigest()
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

// setTitle returns an edit function that sets the title.
func setTitle(title string) func(*audiometa.File) error {
	return func(f *audiometa.File) error {
		f.Tags.Title = title
		return nil
	}
}

func TestWriteID3v1_KeepIsDefault(t *testing.T) {
	t.Parallel()
	const src = "testdata/sample_v24.mp3"
	want := trailerOf(readTestdata(t, src))
	if want == nil {
		t.Fatal("sample_v24.mp3 has no ID3v1 trailer")
	}
	for name, opts := range map[string][]audiometa.WriteOption{
		"default": nil,
		"keep":    {audiometa.WithID3v1(audiometa.ID3v1Keep)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := writeTempCopy(t, src)
			if err := audiometa.Write(p, setTitle("Updated"), opts...); err != nil {
				t.Fatal(err)
			}
			if got := trailerOf(readFileBytes(t, p)); !bytes.Equal(got, want) {
				t.Errorf("trailer changed: %q", got)
			}
		})
	}
}

func TestWriteID3v1_Update(t *testing.T) {
	t.Parallel()
	p := writeTempCopy(t, "testdata/sample_v24.mp3")
	before := audioDigestOf(t, p)
	old := trailerOf(readFileBytes(t, p))

	if err := audiometa.Write(p, setTitle("Updated"), audiometa.WithID3v1(audiometa.ID3v1Update)); err != nil {
		t.Fatal(err)
	}
	got := trailerOf(readFileBytes(t, p))
	if got == nil {
		t.Fatal("no ID3v1 trailer after the write")
	}
	if title := trailerText(got, 3, 33); title != "Updated" {
		t.Errorf("trailer title = %q, want Updated", title)
	}
	if got[127] != old[127] {
		t.Errorf("genre byte = %d, want the old %d", got[127], old[127])
	}
	if after := audioDigestOf(t, p); after != before {
		t.Error("the audio digest changed")
	}
	f := openFile(t, p)
	if f.Tags.Title != "Updated" {
		t.Errorf("Title = %q", f.Tags.Title)
	}
}

func TestWriteID3v1_UpdateWithoutTrailer(t *testing.T) {
	t.Parallel()
	keep := writeTempCopy(t, "testdata/sample.mp3")
	update := writeTempCopy(t, "testdata/sample.mp3")
	if err := audiometa.Write(keep, setTitle("Updated")); err != nil {
		t.Fatal(err)
	}
	if err := audiometa.Write(update, setTitle("Updated"), audiometa.WithID3v1(audiometa.ID3v1Update)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readFileBytes(t, keep), readFileBytes(t, update)) {
		t.Error("Update on a file without a trailer differs from Keep")
	}
	if trailerOf(readFileBytes(t, update)) != nil {
		t.Error("Update added a trailer")
	}
}

func TestWriteID3v1_Remove(t *testing.T) {
	t.Parallel()
	keep := writeTempCopy(t, "testdata/sample_v24.mp3")
	remove := writeTempCopy(t, "testdata/sample_v24.mp3")
	before := audioDigestOf(t, remove)
	if err := audiometa.Write(keep, setTitle("Updated")); err != nil {
		t.Fatal(err)
	}
	if err := audiometa.Write(remove, setTitle("Updated"), audiometa.WithID3v1(audiometa.ID3v1Remove)); err != nil {
		t.Fatal(err)
	}
	k, r := readFileBytes(t, keep), readFileBytes(t, remove)
	if len(r) != len(k)-id3v1Len {
		t.Errorf("size = %d, want %d", len(r), len(k)-id3v1Len)
	}
	if trailerOf(r) != nil {
		t.Error("trailer still present")
	}
	if !bytes.Equal(r, k[:len(k)-id3v1Len]) {
		t.Error("Remove differs from Keep other than by the trailer")
	}
	if after := audioDigestOf(t, remove); after != before {
		t.Error("the audio digest changed")
	}
}

func TestWriteID3v1_ModeAloneWrites(t *testing.T) {
	t.Parallel()
	noEdit := func(*audiometa.File) error { return nil }
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	prepare := func(t *testing.T) string {
		t.Helper()
		p := writeTempCopy(t, "testdata/sample_v24.mp3")
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("remove", func(t *testing.T) {
		t.Parallel()
		p := prepare(t)
		if err := audiometa.Write(p, noEdit, audiometa.WithID3v1(audiometa.ID3v1Remove)); err != nil {
			t.Fatal(err)
		}
		if trailerOf(readFileBytes(t, p)) != nil {
			t.Error("trailer still present")
		}
	})
	t.Run("keep leaves the file untouched", func(t *testing.T) {
		t.Parallel()
		p := prepare(t)
		want := readFileBytes(t, p)
		if err := audiometa.Write(p, noEdit, audiometa.WithID3v1(audiometa.ID3v1Keep)); err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if !st.ModTime().Equal(old) || !bytes.Equal(readFileBytes(t, p), want) {
			t.Error("a Keep write without edits touched the file")
		}
	})
	t.Run("update syncs the trailer to the tags", func(t *testing.T) {
		t.Parallel()
		p := prepare(t)
		// The ID3v2 tag and the trailer of the sample can disagree; the first
		// Update makes them agree, the second has nothing to do.
		if err := audiometa.Write(p, noEdit, audiometa.WithID3v1(audiometa.ID3v1Update)); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		want := readFileBytes(t, p)
		if err := audiometa.Write(p, noEdit, audiometa.WithID3v1(audiometa.ID3v1Update)); err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if !st.ModTime().Equal(old) || !bytes.Equal(readFileBytes(t, p), want) {
			t.Error("a second Update with nothing to change touched the file")
		}
	})
}

func TestWriteID3v1_InPlaceFallsBack(t *testing.T) {
	t.Parallel()
	p := writeTempCopy(t, "testdata/sample_v24.mp3")
	if err := audiometa.Write(p, setTitle("Updated"), audiometa.WithInPlace(), audiometa.WithID3v1(audiometa.ID3v1Remove)); err != nil {
		t.Fatal(err)
	}
	data := readFileBytes(t, p)
	if trailerOf(data) != nil {
		t.Error("trailer still present")
	}
	if f := openFile(t, p); f.Tags.Title != "Updated" {
		t.Errorf("Title = %q", f.Tags.Title)
	}
}

func TestWriteID3v1_InPlaceKeepsWorkingForKeep(t *testing.T) {
	t.Parallel()
	p := writeTempCopy(t, "testdata/sample_v24.mp3")
	want := trailerOf(readFileBytes(t, p))
	if err := audiometa.Write(p, setTitle("Updated"), audiometa.WithInPlace(), audiometa.WithID3v1(audiometa.ID3v1Keep)); err != nil {
		t.Fatal(err)
	}
	if got := trailerOf(readFileBytes(t, p)); !bytes.Equal(got, want) {
		t.Error("trailer changed")
	}
}

func TestWriteID3v1_OtherFormatsIgnoreIt(t *testing.T) {
	t.Parallel()
	p := writeTempCopy(t, "testdata/sample.flac")
	if err := audiometa.Write(p, setTitle("Updated"), audiometa.WithID3v1(audiometa.ID3v1Remove)); err != nil {
		t.Fatal(err)
	}
	if f := openFile(t, p); f.Tags.Title != "Updated" {
		t.Errorf("Title = %q", f.Tags.Title)
	}
}
