package audiometa

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestReconcileExtracted(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	pngData := buf.Bytes()

	tests := []struct {
		name string
		in   Artwork
		want []Artwork // nil: dropped
	}{
		{
			name: "PNG labeled JPEG with no size",
			in:   Artwork{MIMEType: "image/jpeg", Data: pngData},
			want: []Artwork{{MIMEType: "image/png", Width: 3, Height: 2, Data: pngData}},
		},
		{
			name: "sniffed size wins over stored size",
			in:   Artwork{MIMEType: "image/png", Width: 500, Height: 500, Data: pngData},
			want: []Artwork{{MIMEType: "image/png", Width: 3, Height: 2, Data: pngData}},
		},
		{
			name: "unrecognized data keeps stored values",
			in:   Artwork{MIMEType: "image/x-foo", Width: 7, Data: []byte("not an image")},
			want: []Artwork{{MIMEType: "image/x-foo", Width: 7, Data: []byte("not an image")}},
		},
		{
			name: "link picture is dropped",
			in:   Artwork{MIMEType: "-->", Data: []byte("http://example.com/a.png")},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reconcileExtracted([]Artwork{tt.in})
			if len(got) != len(tt.want) {
				t.Fatalf("got %d pictures, want %d", len(got), len(tt.want))
			}
			for i, w := range tt.want {
				g := got[i]
				if g.MIMEType != w.MIMEType || g.Width != w.Width || g.Height != w.Height || !bytes.Equal(g.Data, w.Data) {
					t.Errorf("picture = {%q %dx%d}, want {%q %dx%d}", g.MIMEType, g.Width, g.Height, w.MIMEType, w.Width, w.Height)
				}
			}
		})
	}
}
