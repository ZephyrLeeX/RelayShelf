//go:build integration && thumbnailbench && !race

package files

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"
)

// TestThumbnailBenchmark runs the Phase 11 T126 hardware baseline for the
// thumbnail pipeline on representative inputs. It is excluded from ordinary
// CI (dedicated build tag) and invoked by scripts/hardware-baseline.sh on
// the reference host, where the timing values are the qualification data.
func TestThumbnailBenchmark(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
	}{
		{"phone_photo_4032x3024", 4032, 3024},
		{"screenshot_1920x1080", 1920, 1080},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			decoded := image.NewRGBA(image.Rect(0, 0, testCase.width, testCase.height))
			for y := 0; y < testCase.height; y += 7 {
				for x := 0; x < testCase.width; x += 11 {
					decoded.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 64, A: 255})
				}
			}
			var encoded bytes.Buffer
			if err := jpeg.Encode(&encoded, decoded, &jpeg.Options{Quality: 85}); err != nil {
				t.Fatal(err)
			}
			// Warmup once, then time decode + resize + encode in full passes
			// mirroring the production pipeline shape.
			run := func() time.Duration {
				started := time.Now()
				src, err := jpeg.Decode(bytes.NewReader(encoded.Bytes()))
				if err != nil {
					t.Fatal(err)
				}
				resized := resizeFit(src, ThumbnailMaxEdge)
				var out bytes.Buffer
				if err = jpeg.Encode(&out, resized, &jpeg.Options{Quality: 80}); err != nil {
					t.Fatal(err)
				}
				return time.Since(started)
			}
			run()
			samples := make([]time.Duration, 0, 10)
			for i := 0; i < 10; i++ {
				samples = append(samples, run())
			}
			var total time.Duration
			worst := time.Duration(0)
			for _, sample := range samples {
				total += sample
				if sample > worst {
					worst = sample
				}
			}
			t.Logf("input=%dx%d jpeg_bytes=%d samples=%d mean=%s worst=%s",
				testCase.width, testCase.height, encoded.Len(), len(samples), total/time.Duration(len(samples)), worst)
		})
	}
}
