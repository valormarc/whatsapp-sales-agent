package agent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// Product photos: the pictures a seller throws in ARE her
// goods, so the listing keeps them (his standing rule: one
// product holds many photos, the angles and colours of the
// same item), and a buyer who asks for the pic is sent them.
// The bytes are kept on disk at save time, because a
// WhatsApp media id is not a storage promise; sending
// re-uploads from the kept bytes, the same road the claim
// screenshots already travel.

var (
	downloadMediaBytesFn = whatsapp.DownloadMediaBytes
	uploadMediaFn        = whatsapp.UploadMedia
)

// maxProductPhotos caps how many pictures one listing keeps
// and sends: enough to show the goods, never an album dump.
const maxProductPhotos = 4

// collectFlowPhoto records a photo's media id on the open
// listing, once, up to the cap.
func collectFlowPhoto(flow *OnboardingFlow, imageID string) {
	if imageID == "" {
		return
	}
	for _, id := range flow.PhotoRefs {
		if id == imageID {
			return
		}
	}
	if len(flow.PhotoRefs) < maxProductPhotos {
		flow.PhotoRefs = append(flow.PhotoRefs, imageID)
	}
}

// attachFlowPhotos downloads the listing's collected photos
// to the data directory and records them on the saved
// product. A photo that cannot be fetched is simply not
// kept; the listing itself never waits on pictures.
func attachFlowPhotos(merchantID string, product catalogue.Product, flow *OnboardingFlow) {
	if flow == nil || len(flow.PhotoRefs) == 0 {
		return
	}
	_ = os.MkdirAll(datadir.Path("photos"), 0o755)
	var kept []string
	for i, id := range flow.PhotoRefs {
		data, mime, ok := downloadMediaBytesFn(id)
		if !ok || len(data) == 0 {
			continue
		}
		ext := ".jpg"
		switch {
		case strings.Contains(mime, "png"):
			ext = ".png"
		case strings.Contains(mime, "webp"):
			ext = ".webp"
		}
		rel := filepath.Join("photos", product.ID+"-"+string(rune('1'+i))+ext)
		if err := os.WriteFile(datadir.Path(rel), data, 0o644); err != nil {
			continue
		}
		kept = append(kept, rel)
	}
	if len(kept) > 0 {
		_ = catalogue.SetProductPhotos(merchantID, product.ID, kept)
	}
}

// sendProductPhotos sends a product's kept photos to the
// buyer, the first carrying the product's name as its
// caption, and reports whether at least one went out. With
// no kept photos, or a send that cannot be made, it reports
// false and the caller answers in words, as before.
func sendProductPhotos(from string, p *catalogue.Product) bool {
	if p == nil || len(p.Photos) == 0 {
		return false
	}
	sent := false
	for i, rel := range p.Photos {
		if i >= 3 {
			break
		}
		data, err := os.ReadFile(datadir.Path(rel))
		if err != nil || len(data) == 0 {
			continue
		}
		mime := "image/jpeg"
		switch {
		case strings.HasSuffix(rel, ".png"):
			mime = "image/png"
		case strings.HasSuffix(rel, ".webp"):
			mime = "image/webp"
		}
		ref, ok := uploadMediaFn(data, mime)
		if !ok {
			continue
		}
		caption := ""
		if !sent {
			caption = p.Name
		}
		whatsapp.SendImage(from, ref, caption)
		sent = true
	}
	return sent
}
