package ogg

import (
	"bytes"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func artworkOf(t *testing.T, data []byte) []types.Artwork {
	t.Helper()
	art, err := (&parser{}).ExtractArtwork(t.Context(), bytes.NewReader(data), int64(len(data)), "x.opus")
	if err != nil {
		t.Fatal(err)
	}
	return art
}

func addArt(typ types.ArtworkType, data byte) types.ArtworkOp {
	return types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: types.Artwork{
		MIMEType: "image/jpeg", Type: typ, Description: "d", Width: 1, Height: 1, Data: bytes.Repeat([]byte{data}, 300),
	}}
}

func TestWriteOpusArtwork(t *testing.T) {
	src := readCorpus(t, "sample.opus")
	if len(artworkOf(t, src)) != 0 {
		t.Fatal("the sample has artwork")
	}

	// Add a front cover and a back cover; the large one spans pages.
	big := addArt(types.ArtworkBackCover, 0xBB)
	big.Art.Data = bytes.Repeat([]byte{0xBB}, 90_000)
	out := writeBytes(t, src, editsFor(t, src, nil, addArt(types.ArtworkFrontCover, 0xAA), big))
	checkStructure(t, "added", out)
	preserved(t, "added", src, out)
	art := artworkOf(t, out)
	if len(art) != 2 || art[0].Type != types.ArtworkFrontCover || art[1].Type != types.ArtworkBackCover ||
		!bytes.Equal(art[0].Data, bytes.Repeat([]byte{0xAA}, 300)) || len(art[1].Data) != 90_000 {
		t.Fatalf("artwork after adding = %d images %+v", len(art), art)
	}
	if got := parseOggBytes(t, out).Tags.GetFirst("TITLE"); got != parseOggBytes(t, src).Tags.GetFirst("TITLE") {
		t.Errorf("title changed to %q", got)
	}

	// Remove the front cover by type: the back cover stays.
	rm := types.ArtworkOp{Kind: types.ArtworkOpRemove, Type: types.ArtworkFrontCover}
	out2 := writeBytes(t, out, editsFor(t, out, nil, rm))
	checkStructure(t, "removed one", out2)
	if art := artworkOf(t, out2); len(art) != 1 || art[0].Type != types.ArtworkBackCover {
		t.Errorf("artwork after removing the front cover = %+v", art)
	}

	// Replace: remove then add; the new one goes after the remaining picture.
	out3 := writeBytes(t, out2, editsFor(t, out2, nil, rm, addArt(types.ArtworkFrontCover, 0xCC)))
	if art := artworkOf(t, out3); len(art) != 2 || art[0].Type != types.ArtworkBackCover || art[1].Data[0] != 0xCC {
		t.Errorf("artwork after replacing = %+v", art)
	}

	// Remove all: no picture entry is left, and the file is the size of a tag-only one.
	out4 := writeBytes(t, out3, editsFor(t, out3, nil, types.ArtworkOp{Kind: types.ArtworkOpRemoveAll}))
	checkStructure(t, "removed all", out4)
	preserved(t, "removed all", src, out4)
	if art := artworkOf(t, out4); len(art) != 0 {
		t.Errorf("artwork after removing all = %d images", len(art))
	}
	if !bytes.Equal(out4, src) {
		t.Errorf("removing every picture does not give back the source (%d bytes against %d)", len(out4), len(src))
	}
}

func TestPictureEntryHelpers(t *testing.T) {
	entry := []byte(pictureComment(bytes.Repeat([]byte{1}, 50)))
	if !isPictureEntry(entry) || !isPictureEntry(append([]byte("metadata_block_picture="), "x"...)) {
		t.Error("picture entries not recognized")
	}
	for _, e := range []string{"TITLE=x", "METADATA_BLOCK_PICTURE", "X_METADATA_BLOCK_PICTURE=1"} {
		if isPictureEntry([]byte(e)) {
			t.Errorf("%q taken for a picture", e)
		}
	}
	if typ, ok := pictureEntryType(entry); !ok || typ != types.ArtworkFrontCover {
		t.Errorf("type = %d, %v", typ, ok)
	}
	for _, e := range []string{"METADATA_BLOCK_PICTURE=", "METADATA_BLOCK_PICTURE=AAA", "METADATA_BLOCK_PICTURE=!!!!!!!!"} {
		if _, ok := pictureEntryType([]byte(e)); ok {
			t.Errorf("%q has a type", e)
		}
	}
	if got := lastIndexFunc([]int{1, 2, 3, 2}, func(n int) bool { return n == 2 }); got != 3 {
		t.Errorf("lastIndexFunc = %d", got)
	}
	if got := lastIndexFunc([]int{1}, func(n int) bool { return n == 2 }); got != -1 {
		t.Errorf("lastIndexFunc = %d", got)
	}
}
