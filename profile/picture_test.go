package profile

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testImage makes a w×h image whose left half is red and right half is blue,
// so a centre crop of a wide image keeps both colours and an off-centre crop
// would not.
func testImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{R: 255, A: 255}
			if x >= w/2 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return &buf
}

func TestSetPictureStoresASquarePNGOfFixedSize(t *testing.T) {
	m := openTemp(t)
	p, err := m.Create("Sam", "t")
	if err != nil {
		t.Fatal(err)
	}
	if p.Picture != "" {
		t.Errorf("a new profile should have no picture, got %q", p.Picture)
	}

	// A wide JPEG in: a square PNG of PictureSize out, centre-cropped.
	if err := m.SetPicture("sam", encodeJPEG(t, testImage(1200, 400))); err != nil {
		t.Fatalf("SetPicture: %v", err)
	}
	got, err := m.Get("sam")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(p.Path, PictureFileName)
	if got.Picture != want {
		t.Errorf("Picture = %q, want %q", got.Picture, want)
	}
	f, err := os.Open(want)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("stored picture is not a PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != PictureSize || b.Dy() != PictureSize {
		t.Errorf("stored picture is %dx%d, want %dx%d", b.Dx(), b.Dy(), PictureSize, PictureSize)
	}
	// Centre crop of a red|blue image keeps both halves: left edge red, right edge blue.
	r, _, _, _ := img.At(2, PictureSize/2).RGBA()
	_, _, bl, _ := img.At(PictureSize-3, PictureSize/2).RGBA()
	if r < 0x8000 || bl < 0x8000 {
		t.Errorf("crop is not centred: left red=%d right blue=%d", r>>8, bl>>8)
	}

	// profile.json is untouched by a picture: the file's presence is the fact.
	body, _ := os.ReadFile(filepath.Join(p.Path, FileName))
	if strings.Contains(string(body), "picture") {
		t.Errorf("profile.json should not mention the picture:\n%s", body)
	}
}

func TestSetPictureReplacesAndRemovePictureClears(t *testing.T) {
	m := openTemp(t)
	if _, err := m.Create("Sam", "t"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPicture("sam", encodeJPEG(t, testImage(300, 300))); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPicture("sam", encodeJPEG(t, testImage(64, 64))); err != nil {
		t.Fatalf("replacing a picture: %v", err)
	}
	if err := m.RemovePicture("sam"); err != nil {
		t.Fatalf("RemovePicture: %v", err)
	}
	got, _ := m.Get("sam")
	if got.Picture != "" {
		t.Errorf("Picture should be empty after removal, got %q", got.Picture)
	}
	if err := m.RemovePicture("sam"); err != nil {
		t.Errorf("removing an absent picture should be a no-op, got %v", err)
	}
	if err := m.RemovePicture("nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown slug: err = %v, want ErrNotFound", err)
	}
}

func TestSetPictureRejectsNonImagesAndUnknownSlugs(t *testing.T) {
	m := openTemp(t)
	if _, err := m.Create("Sam", "t"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetPicture("sam", strings.NewReader("this is not an image")); !errors.Is(err, ErrBadPicture) {
		t.Errorf("err = %v, want ErrBadPicture", err)
	}
	if _, err := os.Stat(filepath.Join(m.Dir(), "sam", PictureFileName)); err == nil {
		t.Error("a rejected picture must not leave a file behind")
	}
	if err := m.SetPicture("nobody", encodeJPEG(t, testImage(10, 10))); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown slug: err = %v, want ErrNotFound", err)
	}
}

func TestSquareCropIsCentredForTallImages(t *testing.T) {
	// 100 wide, 300 tall: the centre square is rows 100..200.
	img := image.NewRGBA(image.Rect(0, 0, 100, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.Gray{Y: uint8(y * 255 / 299)})
		}
	}
	sq := squareCrop(img)
	if b := sq.Bounds(); b.Dx() != 100 || b.Dy() != 100 || b.Min.Y != 100 {
		t.Errorf("crop bounds = %v, want 100x100 starting at y=100", b)
	}
}
