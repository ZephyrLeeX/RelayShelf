package files

import (
	"image"
	"image/color"
	"testing"
)

func TestThumbnailDimensionLimits(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		ok   bool
	}{{1, 1, true}, {16384, 1, true}, {0, 2, false}, {16385, 1, false}, {10000, 10000, false}} {
		err := validateDimensions(tc.w, tc.h, ThumbnailMaxDimension, ThumbnailMaxPixels)
		if (err == nil) != tc.ok {
			t.Fatalf("%dx%d err=%v", tc.w, tc.h, err)
		}
	}
}
func TestResizeFitNoUpscaleAndAspect(t *testing.T) {
	small := image.NewNRGBA(image.Rect(0, 0, 300, 200))
	if got := resizeFit(small, 512).Bounds(); got.Dx() != 300 || got.Dy() != 200 {
		t.Fatalf("small resized to %v", got)
	}
	large := image.NewNRGBA(image.Rect(0, 0, 4000, 3000))
	if got := resizeFit(large, 512).Bounds(); got.Dx() != 512 || got.Dy() != 384 {
		t.Fatalf("large resized to %v", got)
	}
}
func TestOrientSix(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.Set(0, 0, color.RGBA{R: 255, A: 255})
	src.Set(1, 0, color.RGBA{B: 255, A: 255})
	got := orient(src, 6)
	if got.Bounds().Dx() != 1 || got.Bounds().Dy() != 2 {
		t.Fatalf("bounds=%v", got.Bounds())
	}
	r, _, _, _ := got.At(0, 0).RGBA()
	_, _, b, _ := got.At(0, 1).RGBA()
	if r == 0 || b == 0 {
		t.Fatal("orientation did not rotate pixels")
	}
}
