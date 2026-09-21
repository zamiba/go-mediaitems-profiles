package profile

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	// Registering decoders is what lets image.Decode recognise the formats a
	// person is likely to hand us. The encoder is always PNG; see PictureFileName.
	_ "image/gif"
	_ "image/jpeg"

	"golang.org/x/image/draw"

	"github.com/zamiba/go-mediaitems/jsonfile"
)

const (
	// PictureFileName is the profile picture, beside profile.json. The name
	// and the format are fixed so that a program showing a list of profiles
	// never has to sniff: a profile either has picture.png or it has none.
	// The file is about the person and travels with the profile - into git,
	// through rclone, onto other devices - like everything else in the folder.
	PictureFileName = "picture.png"

	// PictureSize is the width and height, in pixels, every stored picture
	// has. Programs show it at thirty or forty pixels; at 256 it is sharp on
	// any display and small enough that committing one per profile costs
	// nothing worth noticing.
	PictureSize = 256
)

// ErrBadPicture is returned by SetPicture for input that is not an image the
// package can decode: PNG, JPEG or GIF.
var ErrBadPicture = errors.New("profile: picture is not a PNG, JPEG or GIF image")

// SetPicture stores an image as the profile's picture, replacing any it had.
//
// The stored form is always a PictureSize-square PNG, centre-cropped from the
// input: the crop is decided here rather than by each program so that every
// program in the suite shows the same picture the same way, and so that a
// four-thousand-pixel photograph does not end up committed to git in full
// for a thirty-pixel avatar. Nothing in profile.json changes; the file's
// presence is the fact.
func (m *Manager) SetPicture(slug string, src io.Reader) error {
	p, err := m.Get(slug)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(src)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadPicture, err)
	}
	square := squareCrop(img)

	out := image.NewRGBA(image.Rect(0, 0, PictureSize, PictureSize))
	// CatmullRom is the slow, good scaler; a 256-pixel target makes "slow"
	// tens of milliseconds, and this runs once per picture change.
	draw.CatmullRom.Scale(out, out.Bounds(), square, square.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return fmt.Errorf("profile: encoding picture: %w", err)
	}
	if err := jsonfile.WriteAtomic(filepath.Join(p.Path, PictureFileName), buf.Bytes()); err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	return nil
}

// RemovePicture deletes the profile's picture. A profile with no picture is
// left as it is, without error.
func (m *Manager) RemovePicture(slug string) error {
	p, err := m.Get(slug)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(p.Path, PictureFileName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("profile: removing picture: %w", err)
	}
	return nil
}

// squareCrop returns the largest centred square of img, as a sub-image
// sharing its pixels. A square input comes back unchanged.
func squareCrop(img image.Image) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == h {
		return img
	}
	side := min(w, h)
	x0 := b.Min.X + (w-side)/2
	y0 := b.Min.Y + (h-side)/2
	rect := image.Rect(x0, y0, x0+side, y0+side)

	type subImager interface {
		SubImage(image.Rectangle) image.Image
	}
	if s, ok := img.(subImager); ok {
		return s.SubImage(rect)
	}
	// Every decoder in the standard library returns a type with SubImage;
	// a third-party one might not, so copy in that case rather than fail.
	out := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Copy(out, image.Point{}, img, rect, draw.Src, nil)
	return out
}

// picturePath returns the path of the profile's picture if it has one.
func picturePath(dir string) string {
	path := filepath.Join(dir, PictureFileName)
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}
