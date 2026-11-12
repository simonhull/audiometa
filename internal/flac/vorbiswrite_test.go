package flac

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/vorbis"
)

func TestVC_VerbatimRoundTrip(t *testing.T) {
	payload := vorbisCommentPayload("title=x", "Artist=A", "ARTIST=B", "no separator", "EMPTY=")
	v, err := vorbis.Split(t.Context(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(v.Vendor) != "audiometa-test" || len(v.Entries) != 5 {
		t.Fatalf("vendor %q, %d comments", v.Vendor, len(v.Entries))
	}
	if !bytes.Equal(v.Encode(), payload) {
		t.Error("Encode() differs from the source payload")
	}

	withTail := append(slices.Clone(payload), 1, 2, 3)
	if v, err := vorbis.Split(t.Context(), withTail); err != nil || !bytes.Equal(v.Encode(), withTail) {
		t.Errorf("trailing bytes not kept: err %v", err)
	}
}

func TestVC_Malformed(t *testing.T) {
	good := vorbisCommentPayload("TITLE=A")
	for name, p := range map[string][]byte{
		"empty":           nil,
		"vendor overruns": {0xFF, 0, 0, 0, 'a'},
		"no count":        good[:4+len("audiometa-test")+2],
		"count overruns":  append(good[:4+len("audiometa-test")], 0xFF, 0xFF, 0xFF, 0x0F),
		"comment cut":     good[:len(good)-1],
	} {
		if _, err := vorbis.Split(t.Context(), p); !errors.Is(err, vorbis.ErrTruncated) {
			t.Errorf("%s: err = %v, want vorbis.ErrTruncated", name, err)
		}
	}
}

// writeComments writes edits to simpleFLAC(1024, comments...) and returns
// the output's comments.
func writeComments(t *testing.T, mutate func(*types.Tags), comments ...string) []string {
	t.Helper()
	src := simpleFLAC(1024, comments...)
	return strs(commentsOf(t, writeBytes(t, src, editsFor(t, src, mutate))).Entries)
}

func TestVC_KeyEditPreservesOriginalKeyCase(t *testing.T) {
	got := writeComments(t, func(tg *types.Tags) { tg.Set("TITLE", "New") }, "ARTIST=A", "title=Old", "ALBUM=B")
	if want := []string{"ARTIST=A", "title=New", "ALBUM=B"}; !slices.Equal(got, want) {
		t.Errorf("comments %q, want %q", got, want)
	}
}

func TestVC_MultiValue(t *testing.T) {
	got := writeComments(t, func(tg *types.Tags) { tg.Set("ARTIST", "A", "B") }, "TITLE=T", "ARTIST=Old", "ALBUM=X", "artist=Old2")
	if want := []string{"TITLE=T", "ARTIST=A", "ARTIST=B", "ALBUM=X"}; !slices.Equal(got, want) {
		t.Errorf("comments %q, want %q", got, want)
	}
}

func TestVC_DeleteKey(t *testing.T) {
	got := writeComments(t, func(tg *types.Tags) { tg.Delete("GENRE") }, "Genre=Rock", "TITLE=T", "GENRE=Pop", "genre=Jazz")
	if want := []string{"TITLE=T"}; !slices.Equal(got, want) {
		t.Errorf("comments %q, want %q", got, want)
	}
}

func TestVC_DeleteKeyWithoutField(t *testing.T) {
	got := writeComments(t, func(tg *types.Tags) { tg.Delete("SHOW") }, "TITLE=T", "SHOW=Podcast")
	if want := []string{"TITLE=T"}; !slices.Equal(got, want) {
		t.Errorf("comments %q, want %q", got, want)
	}
}

func TestVC_FieldEdits(t *testing.T) {
	tests := []struct {
		name     string
		comments []string
		mutate   func(*types.Tags)
		want     []string
	}{
		{"title", []string{"TITLE=Old", "ALBUM=A"}, func(tg *types.Tags) { tg.Title = "New" }, []string{"TITLE=New", "ALBUM=A"}},
		{"title added", []string{"ALBUM=A"}, func(tg *types.Tags) { tg.Title = "New" }, []string{"ALBUM=A", "TITLE=New"}},
		{"year keeps month and day", []string{"DATE=2021-05-03"}, func(tg *types.Tags) { tg.Year = 1999 }, []string{"DATE=1999-05-03"}},
		{"year on bare year", []string{"DATE=2021"}, func(tg *types.Tags) { tg.Year = 1999 }, []string{"DATE=1999"}},
		{"year cleared", []string{"DATE=2021", "TITLE=T"}, func(tg *types.Tags) { tg.Year = 0 }, []string{"TITLE=T"}},
		{"track number", []string{"TRACKNUMBER=1"}, func(tg *types.Tags) { tg.TrackNumber = 7 }, []string{"TRACKNUMBER=7"}},
		{"track number cleared", []string{"TRACKNUMBER=1", "TITLE=T"}, func(tg *types.Tags) { tg.TrackNumber = 0 }, []string{"TITLE=T"}},
		{"disc total added", nil, func(tg *types.Tags) { tg.DiscTotal = 2 }, []string{"DISCTOTAL=2"}},
		{"track total via TOTALTRACKS", []string{"TOTALTRACKS=5"}, func(tg *types.Tags) { tg.TrackTotal = 9 }, []string{"TOTALTRACKS=9"}},
		{
			"track total with both keys",
			[]string{"TRACKTOTAL=5", "TOTALTRACKS=5"},
			func(tg *types.Tags) { tg.TrackTotal = 9 },
			[]string{"TRACKTOTAL=9", "TOTALTRACKS=5"},
		},
		{"track total cleared removes both", []string{"TRACKTOTAL=5", "TITLE=T", "TOTALTRACKS=5"}, func(tg *types.Tags) { tg.TrackTotal = 0 }, []string{"TITLE=T"}},
		{"track number keeps its suffix total", []string{"TRACKNUMBER=3/12"}, func(tg *types.Tags) { tg.TrackNumber = 4 }, []string{"TRACKNUMBER=4/12"}},
		{"track total in the suffix", []string{"TRACKNUMBER=3/12"}, func(tg *types.Tags) { tg.TrackTotal = 13 }, []string{"TRACKNUMBER=3/13"}},
		{"track total cleared from the suffix", []string{"TRACKNUMBER=3/12"}, func(tg *types.Tags) { tg.TrackTotal = 0 }, []string{"TRACKNUMBER=3"}},
		{"track number cleared keeps the suffix total", []string{"TRACKNUMBER=3/12"}, func(tg *types.Tags) { tg.TrackNumber = 0 }, []string{"TRACKNUMBER=0/12"}},
		{"both cleared", []string{"TRACKNUMBER=3/12", "TITLE=T"}, func(tg *types.Tags) { tg.TrackNumber, tg.TrackTotal = 0, 0 }, []string{"TITLE=T"}},
		{"explicit total wins over suffix", []string{"TRACKNUMBER=3/12", "TRACKTOTAL=10"}, func(tg *types.Tags) { tg.TrackTotal = 11 }, []string{"TRACKNUMBER=3/12", "TRACKTOTAL=11"}},
		{"explicit total cleared strips the suffix", []string{"TRACKNUMBER=3/12", "TRACKTOTAL=10"}, func(tg *types.Tags) { tg.TrackTotal = 0 }, []string{"TRACKNUMBER=3"}},
		{"disc total in the suffix", []string{"DISCNUMBER=1/2"}, func(tg *types.Tags) { tg.DiscTotal = 3 }, []string{"DISCNUMBER=1/3"}},
		{"series added", []string{"TITLE=T"}, func(tg *types.Tags) { tg.Series = "S" }, []string{"TITLE=T", "SERIES=S"}},
		{"series via MVNM", []string{"MVNM=Old"}, func(tg *types.Tags) { tg.Series = "S" }, []string{"MVNM=S"}},
		{"lyrics", nil, func(tg *types.Tags) { tg.Lyrics = "la la" }, []string{"LYRICS=la la"}},
		{"comment", nil, func(tg *types.Tags) { tg.Comment = "c" }, []string{"COMMENT=c"}},
		{"musicbrainz track", nil, func(tg *types.Tags) { tg.MusicBrainzTrackID = "id" }, []string{"MUSICBRAINZ_TRACKID=id"}},
		{"genres", []string{"GENRE=Old"}, func(tg *types.Tags) { tg.Genres = []string{"A", "B"} }, []string{"GENRE=A", "GENRE=B"}},
		{
			"artist only keeps the other artists",
			[]string{"ARTIST=X", "ARTIST=Y"},
			func(tg *types.Tags) { tg.Artist = "Z" },
			[]string{"ARTIST=Z", "ARTIST=Y"},
		},
		{"artist cleared", []string{"ARTIST=X", "TITLE=T"}, func(tg *types.Tags) { tg.Artist, tg.Artists = "", nil }, []string{"TITLE=T"}},
		{
			"artists",
			[]string{"ARTIST=X"},
			func(tg *types.Tags) { tg.Artist, tg.Artists = "A", []string{"A", "B"} },
			[]string{"ARTIST=A", "ARTIST=B"},
		},
	}
	for _, tt := range tests {
		if got := writeComments(t, tt.mutate, tt.comments...); !slices.Equal(got, tt.want) {
			t.Errorf("%s: comments %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestVC_SuffixTotalsReadBack(t *testing.T) {
	src := simpleFLAC(64, "TRACKNUMBER=3/12", "DISCNUMBER=1/2")
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.TrackNumber, tg.DiscTotal = 4, 3 }))
	g := parseBytes(t, out).Tags
	types.BindFormat(&g, types.FormatFLAC)
	if g.TrackNumber != 4 || g.TrackTotal != 12 || g.DiscNumber != 1 || g.DiscTotal != 3 {
		t.Errorf("read back track %d/%d disc %d/%d, want 4/12 1/3", g.TrackNumber, g.TrackTotal, g.DiscNumber, g.DiscTotal)
	}
}

func TestVC_EveryFieldWritable(t *testing.T) {
	for f := range types.AllFields() {
		v := vorbis.NewEditor(types.FormatFLAC, vorbis.Comments{})
		edits := &types.EditSet{Orig: &types.Tags{}, Final: &types.Tags{Artist: "a", Artists: []string{"a"}}}
		if err := v.ApplyField(types.FieldEdit{Field: f, Values: []string{"1"}}, edits); err != nil || len(v.Entries) == 0 {
			t.Errorf("%s: err %v, %d comments", f, err, len(v.Entries))
		}
	}
}

func TestVC_TypedFieldsReadBack(t *testing.T) {
	src := simpleFLAC(64, "DATE=2021-05-03")
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) {
		tg.Year = 1999
		tg.TrackNumber, tg.TrackTotal = 3, 12
		tg.DiscNumber, tg.DiscTotal = 1, 2
		tg.Narrator = "N"
		tg.SeriesPart = "4"
	}))
	f := parseBytes(t, out)
	if g := f.Tags; g.Year != 1999 || g.Date != "1999-05-03" || g.TrackNumber != 3 || g.TrackTotal != 12 ||
		g.DiscNumber != 1 || g.DiscTotal != 2 || g.Narrator != "N" || g.SeriesPart != "4" {
		t.Errorf("read back %+v", g)
	}
}

func TestVC_InvalidKeyRefused(t *testing.T) {
	for _, key := range []string{"A=B", "TILDE~", "", "NL\n"} {
		src := simpleFLAC(64)
		edits := &types.EditSet{Keys: []types.KeyEdit{{Key: key, Values: []string{"v"}}}, Orig: &types.Tags{}, Final: &types.Tags{}}
		if _, err := tryWrite(src, edits); !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("key %q: err = %v, want ErrUnsupportedWrite", key, err)
		}
	}
	src := simpleFLAC(64)
	edits := &types.EditSet{Keys: []types.KeyEdit{{Key: "TITLE", Values: []string{"\xff"}}}, Orig: &types.Tags{}, Final: &types.Tags{}}
	if _, err := tryWrite(src, edits); !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("invalid UTF-8 value: err = %v, want ErrUnsupportedWrite", err)
	}
}

func TestVC_ChapterKeyRefused(t *testing.T) {
	src := simpleFLAC(64, "CHAPTER001=00:00:00.000", "CHAPTER001NAME=One")
	for name, mutate := range map[string]func(*types.Tags){
		"set":    func(tg *types.Tags) { tg.Set("CHAPTER001NAME", "x") },
		"delete": func(tg *types.Tags) { tg.Delete("CHAPTER001") },
		"lower":  func(tg *types.Tags) { tg.Set("chapter002", "00:01:00.000") },
	} {
		if _, err := tryWrite(src, editsFor(t, src, mutate)); !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("%s: err = %v, want ErrUnsupportedWrite", name, err)
		}
	}
	// CHAPTER not followed by a digit is an ordinary key.
	if got := writeComments(t, func(tg *types.Tags) { tg.Set("CHAPTERS", "x") }); !slices.Equal(got, []string{"CHAPTERS=x"}) {
		t.Errorf("CHAPTERS: comments %q", got)
	}
}

func TestVC_ChaptersPreserved(t *testing.T) {
	got := writeComments(t, func(tg *types.Tags) { tg.Title = "New" }, "CHAPTER001=00:00:00.000", "CHAPTER001NAME=One", "TITLE=T")
	if want := []string{"CHAPTER001=00:00:00.000", "CHAPTER001NAME=One", "TITLE=New"}; !slices.Equal(got, want) {
		t.Errorf("comments %q, want %q", got, want)
	}
}

func TestVC_VendorPreserved(t *testing.T) {
	src := simpleFLAC(64, "TITLE=T")
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) {
		tg.Title = ""
		tg.Set("NEW", "x")
	}))
	if v := commentsOf(t, out); string(v.Vendor) != "audiometa-test" {
		t.Errorf("vendor = %q", v.Vendor)
	}
}

func TestVC_NoCrossFieldWrites(t *testing.T) {
	base := []string{"COMPOSER=C", "GROUPING=Saga #5", "TITLE=Book 3"}
	tests := []struct {
		name   string
		mutate func(*types.Tags)
		added  string
	}{
		{"narrator", func(tg *types.Tags) { tg.Narrator = "N" }, "NARRATOR=N"},
		{"series", func(tg *types.Tags) { tg.Series = "S" }, "SERIES=S"},
		{"series part", func(tg *types.Tags) { tg.SeriesPart = "5" }, "SERIESPART=5"},
	}
	for _, tt := range tests {
		got := writeComments(t, tt.mutate, base...)
		if want := append(slices.Clone(base), tt.added); !slices.Equal(got, want) {
			t.Errorf("%s: comments %q, want %q", tt.name, got, want)
		}
	}
}

func TestVC_PartialOnError(t *testing.T) {
	good := vorbisCommentPayload("TITLE=A", "ARTIST=B")
	v, err := vorbis.Split(t.Context(), good[:len(good)-1]) // the second comment is cut
	if err == nil {
		t.Fatal("no error")
	}
	if string(v.Vendor) != "audiometa-test" || len(v.Entries) != 1 || string(v.Entries[0]) != "TITLE=A" {
		t.Errorf("partial result = %+v, want the vendor and the first comment", v)
	}
	if v, err := vorbis.Split(t.Context(), nil); err == nil || v.Vendor != nil || v.Entries != nil {
		t.Errorf("empty payload: v %+v, err %v; want an empty result and an error", v, err)
	}
}
