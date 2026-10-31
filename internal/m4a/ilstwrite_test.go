package m4a

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// testPNG returns a 1x1 PNG.
func testPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// assertItemsKept checks that every item of before other than the given
// types is in after, byte for byte and in the same relative order.
func assertItemsKept(t *testing.T, before, after []byte, changed ...string) {
	t.Helper()
	keep := func(items [][]byte) [][]byte {
		return slices.DeleteFunc(items, func(it []byte) bool { return slices.Contains(changed, string(it[4:8])) })
	}
	b, a := keep(ilstItems(t, before)), keep(ilstItems(t, after))
	if !slices.EqualFunc(b, a, bytes.Equal) {
		t.Errorf("untouched items changed:\n before %q\n after  %q", b, a)
	}
}

func TestIlst_TextKeyEdit(t *testing.T) {
	src := buildMP4(false, ilstText("\xA9nam", "Old"), ilstText(itemArtist, "Artist"), ilstFreeform("Narrator", "N"))
	out := writeTags(t, src, func(tg *types.Tags) { tg.Set("\xA9nam", "New") })
	if got := parseBytes(t, out).Tags.Title; got != "New" {
		t.Errorf("Title = %q", got)
	}
	if it := ilstItems(t, out); string(it[0][4:8]) != "\xA9nam" {
		t.Error("edited item did not keep its position")
	}
	assertItemsKept(t, src, out, "\xA9nam")
}

func TestIlst_MultiValue(t *testing.T) {
	src := buildMP4(false, ilstText(itemArtist, "Old"))
	out := writeTags(t, src, func(tg *types.Tags) { tg.Artists = []string{"A", "B"}; tg.Artist = "A" })
	if want := ilstText(itemArtist, "A", "B"); !bytes.Equal(itemOf(t, out, itemArtist), want) {
		t.Errorf("item = %x, want two data atoms %x", itemOf(t, out, itemArtist), want)
	}
	tg := parseBytes(t, out).Tags
	if tg.Artist != "A" || !slices.Equal(tg.Artists, []string{"A", "B"}) {
		t.Errorf("Artist = %q, Artists = %q", tg.Artist, tg.Artists)
	}
}

func TestIlst_Freeform(t *testing.T) {
	src := buildMP4(false, ilstText("\xA9nam", "T"), ilstFreeform("Narrator", "Old"))
	out := writeTags(t, src, func(tg *types.Tags) { tg.Set("NARRATOR", "X") })
	if want := ilstFreeform("Narrator", "X"); !bytes.Equal(itemOf(t, out, itemFreeform), want) {
		t.Errorf("freeform = %q, want %q", itemOf(t, out, itemFreeform), want)
	}
	if got := parseBytes(t, out).Tags.Narrator; got != "X" {
		t.Errorf("Narrator = %q", got)
	}

	src = buildMP4(false, ilstText("\xA9nam", "T"))
	out = writeTags(t, src, func(tg *types.Tags) { tg.Set("NARRATOR", "X") })
	if want := ilstFreeform("NARRATOR", "X"); !bytes.Equal(itemOf(t, out, itemFreeform), want) {
		t.Errorf("new freeform = %q, want %q", itemOf(t, out, itemFreeform), want)
	}
	assertItemsKept(t, src, out, itemFreeform)

	if _, err := tryTags(t, src, func(tg *types.Tags) { tg.Set("Mixed", "x") }); !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("lower-case freeform key: err = %v, want ErrUnsupportedWrite", err)
	}
}

func TestIlst_FieldEdits(t *testing.T) {
	tests := []struct {
		name    string
		items   [][]byte
		mutate  func(*types.Tags)
		item    string
		want    []byte // the expected item; nil when wantErr
		wantErr error
	}{
		{"track", nil, func(tg *types.Tags) { tg.TrackNumber, tg.TrackTotal = 3, 12 },
			itemTrkn, ilstInt(itemTrkn, 0, []byte{0, 0, 0, 3, 0, 12, 0, 0}), nil},
		{"disc", nil, func(tg *types.Tags) { tg.DiscNumber, tg.DiscTotal = 1, 2 },
			itemDisk, ilstInt(itemDisk, 0, []byte{0, 0, 0, 1, 0, 2}), nil},
		{"track too large", nil, func(tg *types.Tags) { tg.TrackNumber = 70000 }, itemTrkn, nil, types.ErrUnsupportedWrite},
		{"year keeps date", [][]byte{ilstText(itemDay, "2021-05-03")}, func(tg *types.Tags) { tg.Year = 1999 },
			itemDay, ilstText(itemDay, "1999-05-03"), nil},
		{"year new", nil, func(tg *types.Tags) { tg.Year = 1999 }, itemDay, ilstText(itemDay, "1999"), nil},
		{"series part int", nil, func(tg *types.Tags) { tg.SeriesPart = "4" },
			itemMvi, ilstInt(itemMvi, dataTypeBESigned, []byte{0, 4}), nil},
		{"series part text", nil, func(tg *types.Tags) { tg.SeriesPart = "4.5" },
			itemFreeform, ilstFreeform(keySeriesPart, "4.5"), nil},
		{"series part into mvi refused", [][]byte{ilstInt(itemMvi, dataTypeBESigned, []byte{0, 2})},
			func(tg *types.Tags) { tg.SeriesPart = "4.5" }, itemMvi, nil, types.ErrUnsupportedWrite},
		{"series part existing freeform", [][]byte{ilstFreeform("Series Part", "1")}, func(tg *types.Tags) { tg.SeriesPart = "2" },
			itemFreeform, ilstFreeform("Series Part", "2"), nil},
		{"description canonical", nil, func(tg *types.Tags) { tg.Description = "D" }, "ldes", ilstText("ldes", "D"), nil},
		{"description existing desc", [][]byte{ilstText("desc", "old")}, func(tg *types.Tags) { tg.Description = "D" },
			"desc", ilstText("desc", "D"), nil},
		{"isrc unsupported", nil, func(tg *types.Tags) { tg.ISRC = "X" }, "", nil, types.ErrUnsupportedWrite},
		{"performers unsupported", nil, func(tg *types.Tags) { tg.Performers = []string{"P"} }, "", nil, types.ErrUnsupportedWrite},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := buildMP4(false, append([][]byte{ilstText("\xA9nam", "T")}, tc.items...)...)
			out, err := tryTags(t, src, tc.mutate)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := itemOf(t, out, tc.item); !bytes.Equal(got, tc.want) {
				t.Errorf("item = %x\n want   %x", got, tc.want)
			}
		})
	}
}

func TestIlst_FieldEditsReRead(t *testing.T) {
	src := buildMP4(false, ilstText("\xA9nam", "T"))
	out := writeTags(t, src, func(tg *types.Tags) {
		tg.TrackNumber, tg.TrackTotal, tg.DiscNumber, tg.DiscTotal = 3, 12, 1, 2
		tg.Year, tg.SeriesPart, tg.Series = 1999, "4", "S"
	})
	tg := parseBytes(t, out).Tags
	if tg.TrackNumber != 3 || tg.TrackTotal != 12 || tg.DiscNumber != 1 || tg.DiscTotal != 2 ||
		tg.Year != 1999 || tg.SeriesPart != "4" || tg.Series != "S" {
		t.Errorf("re-read: %+v", tg)
	}
	cleared := writeTags(t, out, func(tg *types.Tags) { tg.TrackNumber, tg.TrackTotal = 0, 0 })
	if itemOf(t, cleared, itemTrkn) != nil {
		t.Error("trkn not removed when number and total are 0")
	}
}

func TestIlst_GenresKeepGnre(t *testing.T) {
	gnre := ilstInt(keyGnre, 0, []byte{0, 18})
	src := buildMP4(false, ilstText("\xA9nam", "T"), gnre)
	out := writeTags(t, src, func(tg *types.Tags) { tg.Genres = []string{"Rock", "Jazz"} })
	if !bytes.Equal(itemOf(t, out, keyGnre), gnre) {
		t.Error("gnre changed by a Genres edit")
	}
	if g := parseBytes(t, out).Tags.Genres; !slices.Equal(g, []string{"Rock", "Jazz"}) {
		t.Errorf("Genres = %q", g)
	}
}

func TestIlst_GnreKey(t *testing.T) {
	src := buildMP4(false, ilstText("\xA9nam", "T"), ilstInt(keyGnre, 0, []byte{0, 18}))
	out := writeTags(t, src, func(tg *types.Tags) { tg.Set(keyGnre, "8") })
	if want := ilstInt(keyGnre, 0, []byte{0, 9}); !bytes.Equal(itemOf(t, out, keyGnre), want) {
		t.Errorf("gnre = %x, want %x", itemOf(t, out, keyGnre), want)
	}
	if got := parseBytes(t, out).Tags.Get(keyGnre); !slices.Equal(got, []string{"8"}) {
		t.Errorf("re-read gnre = %q", got)
	}
	deleted := writeTags(t, src, func(tg *types.Tags) { tg.Delete(keyGnre) })
	if itemOf(t, deleted, keyGnre) != nil {
		t.Error("gnre not deleted")
	}
	assertItemsKept(t, src, deleted, keyGnre)
}

func TestIlst_CreatesMissingPath(t *testing.T) {
	src := buildMP4With(mp4Opts{noUdta: true})
	out := writeTags(t, src, func(tg *types.Tags) { tg.Title = "Created" })
	meta := findPath(moovTreeOf(t, out), atomUdta, atomMeta)
	if meta == nil || !bytes.Equal(meta.full, []byte{0, 0, 0, 0}) {
		t.Fatal("meta not created as a full atom")
	}
	if hdlr := meta.children[0].appendTo(nil); !bytes.Equal(hdlr, mdirHdlr) || len(hdlr) != 33 {
		t.Errorf("hdlr = %x, want %x", hdlr, mdirHdlr)
	}
	if meta.children[1].typ != atomIlst {
		t.Error("ilst does not follow hdlr")
	}
	if got := parseBytes(t, out).Tags.Title; got != "Created" {
		t.Errorf("Title = %q", got)
	}

	// Deleting nothing that exists creates nothing.
	edits := editsFor(t, src, nil, types.ArtworkOp{Kind: types.ArtworkOpRemoveAll})
	if out := writeBytes(t, src, edits); findPath(moovTreeOf(t, out), atomUdta) != nil {
		t.Error("udta created for an edit that writes nothing")
	}
}

func TestIlst_DeleteEveryKeyKeepsEmptyIlst(t *testing.T) {
	src := buildMP4(false, ilstText("\xA9nam", "T"), ilstText(itemArtist, "A", "B"), ilstFreeform("CUSTOM", "c"),
		ilstInt("tmpo", dataTypeBESigned, []byte{0, 90}), ilstInt(keyGnre, 0, []byte{0, 1}))
	out := writeTags(t, src, func(tg *types.Tags) {
		var keys []string
		for k := range tg.All() {
			keys = append(keys, k)
		}
		tg.Delete(keys...)
	})
	ilst := findPath(moovTreeOf(t, out), atomUdta, atomMeta, atomIlst)
	if ilst == nil || len(ilst.children) != 0 {
		t.Fatalf("ilst = %+v, want present and empty", ilst)
	}
	for k, v := range parseBytes(t, out).Tags.All() {
		t.Errorf("key %q = %q survived", k, v)
	}
}

func TestCovr_Ops(t *testing.T) {
	jpeg := covrImage{covrJPEG, []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2}}
	src := buildMP4(false, ilstText("\xA9nam", "T"), covrItem(jpeg))
	pngData := testPNG(t)
	front := types.Artwork{MIMEType: mimeTypePNG, Data: pngData, Type: types.ArtworkFrontCover}

	out := writeTags(t, src, nil,
		types.ArtworkOp{Kind: types.ArtworkOpRemove, Type: types.ArtworkFrontCover}, types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: front})
	if art := artworkOf(t, out); len(art) != 1 || !bytes.Equal(art[0].Data, pngData) || art[0].MIMEType != mimeTypePNG {
		t.Errorf("replace: %d images", len(art))
	}
	if it := ilstItems(t, out); string(it[1][4:8]) != itemCovr {
		t.Error("replaced covr moved")
	}

	out = writeTags(t, src, nil, types.ArtworkOp{Kind: types.ArtworkOpRemoveAll})
	if itemOf(t, out, itemCovr) != nil {
		t.Error("covr kept after RemoveAll")
	}

	out = writeTags(t, src, nil, types.ArtworkOp{Kind: types.ArtworkOpRemove, Type: types.ArtworkBackCover})
	if !bytes.Equal(itemOf(t, out, itemCovr), covrItem(jpeg)) {
		t.Error("Remove(BackCover) changed covr")
	}

	out = writeTags(t, src, nil, types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: front})
	if want := covrItem(jpeg, covrImage{covrPNG, pngData}); !bytes.Equal(itemOf(t, out, itemCovr), want) {
		t.Error("add did not append a PNG data atom after the untouched JPEG")
	}

	for name, a := range map[string]types.Artwork{
		"back cover": {MIMEType: mimeTypePNG, Data: pngData, Type: types.ArtworkBackCover},
		"gif":        {MIMEType: "image/gif", Data: []byte("GIF89a"), Type: types.ArtworkFrontCover},
	} {
		if _, err := tryTags(t, src, nil, types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: a}); !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("%s: err = %v, want ErrUnsupportedWrite", name, err)
		}
	}

	out = writeTags(t, src, func(tg *types.Tags) { tg.Title = "Other" })
	if !bytes.Equal(itemOf(t, out, itemCovr), covrItem(jpeg)) {
		t.Error("covr changed by a text edit")
	}

	noCovr := buildMP4(false, ilstText("\xA9nam", "T"))
	out = writeTags(t, noCovr, nil, types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: front})
	if it := ilstItems(t, out); len(it) != 2 || string(it[1][4:8]) != itemCovr {
		t.Error("new covr not appended at the end of ilst")
	}
}

func TestIlst_NoCrossFieldWrites(t *testing.T) {
	items := [][]byte{ilstText("\xA9wrt", "C"), ilstText("\xA9grp", "Saga #5"), ilstText("\xA9nam", "Book 3")}
	src := buildMP4(false, items...)

	out := writeTags(t, src, func(tg *types.Tags) { tg.Narrator = "N" })
	got := ilstItems(t, out)
	if !slices.EqualFunc(got[:3], items, bytes.Equal) {
		t.Error("Narrator edit changed an existing item")
	}
	if len(got) != 4 || !bytes.Equal(got[3], ilstFreeform("NARRATOR", "N")) {
		t.Errorf("Narrator edit: items %q, want only an added NARRATOR", got)
	}

	out = writeTags(t, src, func(tg *types.Tags) { tg.Series = "S" })
	for _, typ := range []string{"\xA9wrt", "\xA9grp", "\xA9nam"} {
		if !bytes.Equal(itemOf(t, out, typ), itemOf(t, src, typ)) {
			t.Errorf("Series edit changed %q", typ)
		}
	}
	if want := ilstText("\xA9mvn", "S"); !bytes.Equal(itemOf(t, out, "\xA9mvn"), want) {
		t.Error("Series not written to \xA9mvn")
	}
}

func TestIlst_IntegerItemEdit(t *testing.T) {
	src := buildMP4(false, ilstInt("tmpo", dataTypeBESigned, []byte{0, 90}))
	out := writeTags(t, src, func(tg *types.Tags) { tg.Set("tmpo", "120") })
	if want := ilstInt("tmpo", dataTypeBESigned, []byte{0, 0x78}); !bytes.Equal(itemOf(t, out, "tmpo"), want) {
		t.Errorf("tmpo = %x, want %x", itemOf(t, out, "tmpo"), want)
	}
	for _, v := range []string{"fast", "40000"} {
		if _, err := tryTags(t, src, func(tg *types.Tags) { tg.Set("tmpo", v) }); !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("tmpo %q: err = %v, want ErrUnsupportedWrite", v, err)
		}
	}
	u := buildMP4(false, ilstInt("cpil", dataTypeBEUnsigned, []byte{1}))
	if _, err := tryTags(t, u, func(tg *types.Tags) { tg.Set("cpil", "-1") }); !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("negative unsigned: err = %v", err)
	}
}

func TestIlst_RefusedKeys(t *testing.T) {
	src := buildMP4(false, ilstText("\xA9nam", "T"), covrItem(covrImage{covrJPEG, []byte{1}}),
		atom("xyzw", dataAtom(13, []byte{1, 2})))
	for _, key := range []string{itemTrkn, itemDisk, itemCovr, itemFreeform, "xyzw"} {
		if _, err := tryTags(t, src, func(tg *types.Tags) { tg.Set(key, "1") }); !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("Set(%q): err = %v, want ErrUnsupportedWrite", key, err)
		}
	}
	if _, err := tryTags(t, src, func(tg *types.Tags) { tg.Title = "\xff" }); !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("invalid UTF-8: err = %v", err)
	}
}
