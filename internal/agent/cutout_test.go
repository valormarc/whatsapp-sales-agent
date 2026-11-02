package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// The cutout road: when the service is configured, a
// listing's photo is replaced by the service's cutout of
// the product, the original is kept untouched beside it,
// and every failure along the road leaves the listing
// exactly as it was.

// makeCutoutJPEG builds a real, decodable JPEG comfortably
// past the plausibility floor, to stand in for a service
// cutout.
func makeCutoutJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 96, 96))
	for y := 0; y < 96; y++ {
		for x := 0; x < 96; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 2), uint8(y * 2), 200, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode stand-in cutout: %v", err)
	}
	if buf.Len() < 512 {
		t.Fatalf("stand-in cutout too small to be plausible: %d bytes", buf.Len())
	}
	return buf.Bytes()
}

func stubSegment(t *testing.T, fn func(ctx context.Context, photo []byte, prompt, format string) ([]byte, bool)) {
	t.Helper()
	old := segmentCutoutFn
	segmentCutoutFn = fn
	t.Cleanup(func() { segmentCutoutFn = old })
}

// saveSlidesListing runs the compact photo listing from
// the product photo tests: an advert, one photo, the two
// prices. It returns the chat's data dir.
func saveSlidesListing(t *testing.T, owner string) string {
	t.Helper()
	stubPhotoMedia(t)
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}
	c.say(t, owner, "B Buckle Slides\n\nSizes 37 to 42")
	c.replies(t, owner)
	HandleInbound(Inbound{From: owner, ImageID: "slides-a"})
	c.replies(t, owner)
	c.say(t, owner, "75")
	c.replies(t, owner)
	c.say(t, owner, "70")
	saved := strings.Join(c.replies(t, owner), "\n")
	if strings.Contains(saved, "by measure") {
		c.say(t, owner, "whole")
		saved = strings.Join(c.replies(t, owner), "\n")
	}
	if !strings.Contains(saved, "Saved ✅ B Buckle Slides - GH₵75, last price GH₵70") {
		t.Fatalf("the compact save did not land: %q", saved)
	}
	return dir
}

func listedSlides(t *testing.T) *catalogue.Product {
	t.Helper()
	shop := catalogue.GetMerchant("demo")
	if shop == nil {
		t.Fatal("the demo shop is missing")
	}
	for _, p := range catalogue.ListProducts(shop.ID) {
		if strings.Contains(p.Name, "Buckle Slides") {
			pp := p
			return &pp
		}
	}
	t.Fatal("the slides were never listed")
	return nil
}

func TestCutoutTakesTheListingsPhotoPlace(t *testing.T) {
	t.Setenv("SAM_SEGMENT_URL", "http://segment.invalid/cut")
	cut := makeCutoutJPEG(t)
	var gotPrompt, gotFormat string
	var gotPhoto []byte
	stubSegment(t, func(ctx context.Context, photo []byte, prompt, format string) ([]byte, bool) {
		gotPhoto, gotPrompt, gotFormat = photo, prompt, format
		return cut, true
	})
	owner := "233550003101"
	dir := saveSlidesListing(t, owner)

	slides := listedSlides(t)
	if len(slides.Photos) != 1 {
		t.Fatalf("the listing should keep its one photo, got %v", slides.Photos)
	}
	rel := slides.Photos[0]
	stored, err := os.ReadFile(filepath.Join(dir, "data", rel))
	if err != nil {
		t.Fatalf("read the stored photo: %v", err)
	}
	if !bytes.Equal(stored, cut) {
		t.Fatal("the stored photo is not the cutout the service returned")
	}
	orig, err := os.ReadFile(filepath.Join(dir, "data", originalPhotoRel(rel)))
	if err != nil {
		t.Fatalf("the original was not retained beside the cutout: %v", err)
	}
	if string(orig) != "fake-jpeg-bytes-slides-a" {
		t.Fatalf("the retained original is not the seller's photo: %q", orig)
	}
	if string(gotPhoto) != "fake-jpeg-bytes-slides-a" {
		t.Fatalf("the service was not sent the seller's photo: %q", gotPhoto)
	}
	if gotPrompt != slides.Name {
		t.Fatalf("the prompt should be the listing's own name %q, got %q", slides.Name, gotPrompt)
	}
	if gotFormat != "jpeg" {
		t.Fatalf("the format should ride along as jpeg, got %q", gotFormat)
	}
}

func TestCutoutOffWithoutAnEndpoint(t *testing.T) {
	os.Unsetenv("SAM_SEGMENT_URL")
	called := false
	stubSegment(t, func(ctx context.Context, photo []byte, prompt, format string) ([]byte, bool) {
		called = true
		return makeCutoutJPEG(t), true
	})
	owner := "233550003102"
	dir := saveSlidesListing(t, owner)

	slides := listedSlides(t)
	rel := slides.Photos[0]
	stored, err := os.ReadFile(filepath.Join(dir, "data", rel))
	if err != nil {
		t.Fatalf("read the stored photo: %v", err)
	}
	if string(stored) != "fake-jpeg-bytes-slides-a" {
		t.Fatalf("with no endpoint the photo should stay as taken, got %d bytes", len(stored))
	}
	if called {
		t.Fatal("the service was called with no endpoint configured")
	}
	if _, err := os.Stat(filepath.Join(dir, "data", originalPhotoRel(rel))); !os.IsNotExist(err) {
		t.Fatal("no original sibling should exist when no cutout was made")
	}
}

func TestCutoutFailureKeepsTheOriginal(t *testing.T) {
	t.Setenv("SAM_SEGMENT_URL", "http://segment.invalid/cut")
	stubSegment(t, func(ctx context.Context, photo []byte, prompt, format string) ([]byte, bool) {
		return nil, false
	})
	owner := "233550003103"
	dir := saveSlidesListing(t, owner)

	slides := listedSlides(t)
	rel := slides.Photos[0]
	stored, err := os.ReadFile(filepath.Join(dir, "data", rel))
	if err != nil {
		t.Fatalf("read the stored photo: %v", err)
	}
	if string(stored) != "fake-jpeg-bytes-slides-a" {
		t.Fatal("a failed cutout must leave the seller's photo exactly as taken")
	}
	if _, err := os.Stat(filepath.Join(dir, "data", originalPhotoRel(rel))); !os.IsNotExist(err) {
		t.Fatal("no original sibling should exist when the cutout failed")
	}
}

func TestCutoutLandsInTheBackgroundAfterTheGrace(t *testing.T) {
	t.Setenv("SAM_SEGMENT_URL", "http://segment.invalid/cut")
	oldGrace := cutoutGrace
	cutoutGrace = 50 * time.Millisecond
	t.Cleanup(func() { cutoutGrace = oldGrace })
	cut := makeCutoutJPEG(t)
	release := make(chan struct{})
	stubSegment(t, func(ctx context.Context, photo []byte, prompt, format string) ([]byte, bool) {
		<-release
		return cut, true
	})
	owner := "233550003104"
	dir := saveSlidesListing(t, owner)

	slides := listedSlides(t)
	rel := slides.Photos[0]
	stored, err := os.ReadFile(filepath.Join(dir, "data", rel))
	if err != nil {
		t.Fatalf("read the stored photo: %v", err)
	}
	if string(stored) != "fake-jpeg-bytes-slides-a" {
		t.Fatal("the save should have carried on with the original once the grace ran out")
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		stored, err = os.ReadFile(filepath.Join(dir, "data", rel))
		if err == nil && bytes.Equal(stored, cut) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !bytes.Equal(stored, cut) {
		t.Fatal("the cutout never swapped in in the background")
	}
	orig, err := os.ReadFile(filepath.Join(dir, "data", originalPhotoRel(rel)))
	if err != nil || string(orig) != "fake-jpeg-bytes-slides-a" {
		t.Fatalf("the background swap must retain the original too: %v", err)
	}
}

func TestSegmentRequestContract(t *testing.T) {
	cut := makeCutoutJPEG(t)
	var gotImage, gotPrompt, gotFormat string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ImageB64 string `json:"image_b64"`
			Prompt   string `json:"prompt"`
			Format   string `json:"format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		raw, err := base64.StdEncoding.DecodeString(body.ImageB64)
		if err != nil {
			t.Errorf("decode image_b64: %v", err)
		}
		gotImage, gotPrompt, gotFormat = string(raw), body.Prompt, body.Format
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"cutout_b64": base64.StdEncoding.EncodeToString(cut),
		})
	}))
	defer srv.Close()
	t.Setenv("SAM_SEGMENT_URL", srv.URL)

	got, ok := requestSegmentCutout(context.Background(), []byte("the-photo"), "ankara cloth", "jpeg")
	if !ok || !bytes.Equal(got, cut) {
		t.Fatalf("the JSON reply should come back as the cutout: ok=%v len=%d", ok, len(got))
	}
	if gotImage != "the-photo" || gotPrompt != "ankara cloth" || gotFormat != "jpeg" {
		t.Fatalf("the request carried %q / %q / %q", gotImage, gotPrompt, gotFormat)
	}
}

func TestSegmentRequestFailures(t *testing.T) {
	cut := makeCutoutJPEG(t)
	cases := map[string]http.HandlerFunc{
		"server error": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
		"empty cutout": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"cutout_b64": ""})
		},
		"not json": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("hello"))
		},
		"raw image": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(cut)
		},
	}
	for name, handler := range cases {
		srv := httptest.NewServer(handler)
		t.Setenv("SAM_SEGMENT_URL", srv.URL)
		got, ok := requestSegmentCutout(context.Background(), []byte("the-photo"), "cloth", "jpeg")
		srv.Close()
		if name == "raw image" {
			if !ok || !bytes.Equal(got, cut) {
				t.Fatalf("%s: a raw image reply should be accepted", name)
			}
			continue
		}
		if ok {
			t.Fatalf("%s: the call should report failure", name)
		}
	}
}

func TestPlausibleCutout(t *testing.T) {
	cut := makeCutoutJPEG(t)
	if !plausibleCutout(cut, "jpeg") {
		t.Fatal("a real JPEG should be plausible")
	}
	if plausibleCutout([]byte("tiny"), "jpeg") {
		t.Fatal("a handful of bytes is never a composite")
	}
	if plausibleCutout(bytes.Repeat([]byte("x"), 1024), "jpeg") {
		t.Fatal("a kilobyte of noise is not a JPEG")
	}
	webp := append([]byte("RIFF"), 0, 0, 0, 0)
	webp = append(webp, []byte("WEBP")...)
	webp = append(webp, bytes.Repeat([]byte("y"), 600)...)
	if !plausibleCutout(webp, "webp") {
		t.Fatal("a RIFF/WEBP body of real size should be plausible")
	}
}
