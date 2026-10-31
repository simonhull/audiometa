package audiometa

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/simonhull/audiometa/internal/imagemeta"
	"github.com/simonhull/audiometa/internal/types"
)

// AddArtwork adds the image a to the file's pictures, after any existing
// ones; a.Type is its picture type, such as [ArtworkFrontCover].
//
// The edit is pending: it is applied, in call order with the other artwork
// edits, by [File.WriteTo] or [Write]. [File.ExtractArtwork] keeps reporting
// the artwork stored in the file. AddArtwork copies a.Data, so the caller may
// reuse the buffer. When a.MIMEType is empty it is detected from the data,
// and when Width and Height are both 0 they are read from JPEG or PNG data.
//
// Some formats restrict pictures: M4A/M4B stores no picture type, and every
// format accepts only certain image types. Edits a format cannot store make
// WriteTo return an error wrapping [ErrUnsupportedWrite].
//
// AddArtwork is safe for concurrent use.
//
// # Errors
//
// AddArtwork returns an error wrapping [ErrInvalidArtwork], and records
// nothing, when a.Data is empty, a.Type is not a valid [ArtworkType],
// a.Description is not valid UTF-8, or a.MIMEType is empty and the data is
// not a recognized image (JPEG, PNG, GIF, BMP or WebP).
func (f *File) AddArtwork(a Artwork) error {
	art, err := normalizeArtwork(a)
	if err != nil {
		return err
	}
	f.appendArtworkOps(types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: art})
	return nil
}

// ReplaceArtwork removes every picture of type a.Type, then adds a in their
// place: [File.RemoveArtwork](a.Type) followed by [File.AddArtwork](a), as
// one edit. Use it to set the front cover.
//
// Like AddArtwork, the edit is pending until [File.WriteTo] or [Write];
// [File.ExtractArtwork] keeps reporting the artwork stored in the file; and
// formats that restrict pictures (M4A/M4B stores no picture type) report
// edits they cannot store as [ErrUnsupportedWrite] from WriteTo.
//
// ReplaceArtwork is safe for concurrent use.
//
// # Errors
//
// ReplaceArtwork validates a exactly as [File.AddArtwork] does and returns an
// error wrapping [ErrInvalidArtwork], recording no edit at all, when a is
// rejected.
func (f *File) ReplaceArtwork(a Artwork) error {
	art, err := normalizeArtwork(a)
	if err != nil {
		return err
	}
	f.appendArtworkOps(
		types.ArtworkOp{Kind: types.ArtworkOpRemove, Type: art.Type},
		types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: art},
	)
	return nil
}

// RemoveArtwork removes every picture of type t. Removing a type the file
// does not have is not an error.
//
// The edit is pending until [File.WriteTo] or [Write];
// [File.ExtractArtwork] keeps reporting the artwork stored in the file.
// M4A/M4B stores no picture type, so there every picture counts as
// [ArtworkFrontCover]; other edits a format cannot store are reported as
// [ErrUnsupportedWrite] by WriteTo.
//
// RemoveArtwork is safe for concurrent use.
func (f *File) RemoveArtwork(t ArtworkType) {
	f.appendArtworkOps(types.ArtworkOp{Kind: types.ArtworkOpRemove, Type: t})
}

// RemoveAllArtwork removes every picture from the file.
//
// The edit is pending until [File.WriteTo] or [Write];
// [File.ExtractArtwork] keeps reporting the artwork stored in the file.
// Edits a format cannot store are reported as [ErrUnsupportedWrite] by
// WriteTo.
//
// RemoveAllArtwork is safe for concurrent use.
func (f *File) RemoveAllArtwork() {
	f.appendArtworkOps(types.ArtworkOp{Kind: types.ArtworkOpRemoveAll})
}

// appendArtworkOps records ops as pending artwork edits.
func (f *File) appendArtworkOps(ops ...types.ArtworkOp) {
	f.editMu.Lock()
	defer f.editMu.Unlock()
	f.artworkOps = append(f.artworkOps, ops...)
}

// pendingArtwork returns a copy of the pending artwork edits.
func (f *File) pendingArtwork() []types.ArtworkOp {
	f.editMu.Lock()
	defer f.editMu.Unlock()
	return slices.Clone(f.artworkOps)
}

// normalizeArtwork validates a and returns a copy with its own Data, a
// detected MIMEType when none was given, and sniffed dimensions when none
// were given.
func normalizeArtwork(a Artwork) (Artwork, error) {
	switch {
	case len(a.Data) == 0:
		return Artwork{}, fmt.Errorf("%w: no image data", ErrInvalidArtwork)
	case a.Type < ArtworkOther || a.Type > ArtworkPublisherLogotype:
		return Artwork{}, fmt.Errorf("%w: invalid picture type %d", ErrInvalidArtwork, int(a.Type))
	case !utf8.ValidString(a.Description):
		return Artwork{}, fmt.Errorf("%w: description is not valid UTF-8", ErrInvalidArtwork)
	}
	if a.MIMEType == "" {
		a.MIMEType = imagemeta.MIMEType(a.Data)
		if a.MIMEType == "" {
			return Artwork{}, fmt.Errorf("%w: MIME type not given and not detectable from the data", ErrInvalidArtwork)
		}
	}
	if a.Width == 0 && a.Height == 0 {
		a.Width, a.Height = imagemeta.Dimensions(a.Data, a.MIMEType)
	}
	a.Data = slices.Clone(a.Data)
	return a, nil
}
