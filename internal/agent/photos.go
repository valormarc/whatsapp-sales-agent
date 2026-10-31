package agent

import (
	"os"
	"path/filepath"
	"strconv"
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
// re-uploads from the kept bytes every time.

var (
	downloadMediaBytesFn = whatsapp.DownloadMediaBytes
	uploadMediaFn        = whatsapp.UploadMedia
)

// maxProductPhotos caps how many pictures one listing keeps
// and sends: enough to show the goods in every colour a
// seller forwards, never an album dump.
const maxProductPhotos = 6

// applyColourRule shapes a photo listing's name and
// description at save when its photos showed one item in
// several colours. A name carrying one of those colours
// ("White Maxi Dress" on a listing holding the white, the
// black and the brown) advertises only that colour and
// hides the rest from buyers searching by name, so the
// colour comes out of the name and the colours are listed
// in the description instead, where every colour is
// findable. One colour across all photos leaves both
// untouched: there the colour truly is the product. The
// colours themselves are the photo reads' own reports;
// this code only compares and joins them.
func applyColourRule(flow *OnboardingFlow, name, description string) (string, string) {
	if flow == nil {
		return name, description
	}
	return applyColourRuleFor(flow.ColoursSeen, name, description)
}

// applyColourRuleFor is the colour rule over an explicit set
// of colours read from one item's photos; a burst item carries
// its own set, apart from any open listing (see burst.go).
func applyColourRuleFor(colours []string, name, description string) (string, string) {
	if len(colours) < 2 {
		return name, description
	}
	fields := strings.Fields(name)
	for _, colour := range colours {
		cf := strings.Fields(colour)
		if len(cf) == 0 || len(cf) > len(fields) {
			continue
		}
		for i := 0; i+len(cf) <= len(fields); i++ {
			match := true
			for j, w := range cf {
				if strings.ToLower(strings.Trim(fields[i+j], ".,-")) != w {
					match = false
					break
				}
			}
			if match {
				fields = append(fields[:i], fields[i+len(cf):]...)
				break
			}
		}
	}
	if len(fields) > 0 {
		name = strings.Join(fields, " ")
	}
	if !strings.Contains(strings.ToLower(description), "comes in") {
		line := "Comes in " + joinColourList(colours) + "."
		trimmed := strings.TrimSpace(description)
		if trimmed != "" && !strings.HasSuffix(trimmed, ".") {
			description = trimmed + ". "
		} else if trimmed != "" {
			description = trimmed + " "
		}
		description += line
	}
	return name, description
}

// joinColourList renders colours as a buyer reads them:
// "white, black and brown".
func joinColourList(colours []string) string {
	switch len(colours) {
	case 0:
		return ""
	case 1:
		return colours[0]
	case 2:
		return colours[0] + " and " + colours[1]
	default:
		return strings.Join(colours[:len(colours)-1], ", ") + " and " + colours[len(colours)-1]
	}
}

// collectFlowPhoto records a photo's media id on the open
// listing, once, up to the cap. It reports whether the photo
// is on the listing afterwards: an acknowledgement may only
// say a photo was added when this says it was.
func collectFlowPhoto(flow *OnboardingFlow, imageID string) bool {
	if imageID == "" {
		return false
	}
	for _, id := range flow.PhotoRefs {
		if id == imageID {
			return true
		}
	}
	if len(flow.PhotoRefs) < maxProductPhotos {
		flow.PhotoRefs = append(flow.PhotoRefs, imageID)
		return true
	}
	return false
}

// attachFlowPhotos downloads the listing's collected photos
// to the data directory and records them on the saved
// product. A photo that cannot be fetched is simply not
// kept; the listing itself never waits on pictures.
func attachFlowPhotos(merchantID string, product catalogue.Product, flow *OnboardingFlow) {
	if flow == nil {
		return
	}
	attachPhotos(merchantID, product, flow.PhotoRefs)
}

// attachPhotos is attachFlowPhotos over an explicit set of
// media ids; a burst item carries its own set (see burst.go).
func attachPhotos(merchantID string, product catalogue.Product, refs []string) {
	if len(refs) == 0 {
		return
	}
	_ = os.MkdirAll(datadir.Path(filepath.Join("photos", merchantID)), 0o755)
	var kept []string
	for i, id := range refs {
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
		rel := scopedPhotoRel(merchantID, product.ID, i+1, ext)
		if err := os.WriteFile(datadir.Path(rel), data, 0o600); err != nil {
			continue
		}
		kept = append(kept, rel)
	}
	if len(kept) > 0 {
		_ = catalogue.SetProductPhotos(merchantID, product.ID, kept)
	}
}

// scopedPhotoRel is where a product's photo lives: under
// its own shop's directory, so two shops selling a product
// with the same name (and therefore the same product id)
// can never overwrite or serve each other's pictures —
// the collision the 2026-10-30 audit demonstrated with
// two shops' beaded bags.
func scopedPhotoRel(merchantID, productID string, n int, ext string) string {
	return filepath.Join("photos", merchantID, productID+"-"+strconv.Itoa(n)+ext)
}

// nextPhotoSlot is the slot a new photo for a product
// takes: one past the HIGHEST slot its record already
// holds, never the count of its photos. Slots are minted
// by source index in attachPhotos, so a failed download
// leaves a gap (photos 1 and 3, say) — and the third
// audit found a count-derived slot (3) overwriting the
// surviving photo at that slot while its record still
// pointed there.
func nextPhotoSlot(p *catalogue.Product) int {
	max := 0
	prefix := p.ID + "-"
	for _, rel := range p.Photos {
		base := filepath.Base(rel)
		if !strings.HasPrefix(base, prefix) {
			continue
		}
		rest := strings.TrimPrefix(base, prefix)
		if i := strings.Index(rest, "."); i > 0 {
			if n, err := strconv.Atoi(rest[:i]); err == nil && n > max {
				max = n
			}
		}
	}
	return max + 1
}

// readStoredPhoto reads one kept photo. A path recorded
// before shop scoping (a flat photos/<id>-<n> path) is
// migrated on first read: the file moves under its shop's
// directory and the listing's record follows, so the old
// shared namespace drains away on its own.
func readStoredPhoto(shopID string, p *catalogue.Product, rel string) ([]byte, bool) {
	if data, err := os.ReadFile(datadir.Path(rel)); err == nil && len(data) > 0 {
		return data, true
	}
	// Legacy flat path: try the shop-scoped home of the
	// same file name and migrate it there.
	base := filepath.Base(rel)
	scoped := filepath.Join("photos", shopID, base)
	if scoped == rel {
		return nil, false
	}
	data, err := os.ReadFile(datadir.Path(scoped))
	if err == nil && len(data) > 0 {
		return data, true
	}
	legacy := filepath.Join("photos", base)
	data, err = os.ReadFile(datadir.Path(legacy))
	if err != nil || len(data) == 0 {
		return nil, false
	}
	_ = os.MkdirAll(datadir.Path(filepath.Join("photos", shopID)), 0o755)
	if err := os.WriteFile(datadir.Path(scoped), data, 0o600); err == nil {
		photos := append([]string{}, p.Photos...)
		for i, r := range photos {
			if r == rel {
				photos[i] = scoped
			}
		}
		_ = catalogue.SetProductPhotos(shopID, p.ID, photos)
		// The legacy file is this reader's copy source,
		// not hers to take away: a flat path from before
		// scoping can be named by ANOTHER shop's product
		// too (the run-1 collision), and the old move
		// deleted it from under that shop (second audit,
		// needs-validation). It is removed only once no
		// product anywhere still names it.
		if !catalogue.PhotoReferenced(legacy) {
			_ = os.Remove(datadir.Path(legacy))
		}
	}
	return data, true
}

// sendProductPhotos sends a product's kept photos to the
// buyer, the first carrying the product's name as its
// caption, and reports whether at least one went out. With
// no kept photos, or a send that cannot be made, it reports
// false and the caller answers in words, as before.
//
// The product is re-resolved against the live catalogue
// first: a session-held copy of a product the merchant has
// since removed (or re-photographed) must never send the
// old pictures — the control servePendingPicRequest always
// had, now kept by every reader.
func sendProductPhotos(from, shopID string, p *catalogue.Product) bool {
	if p == nil {
		return false
	}
	live := catalogue.FindByID(shopID, p.ID)
	if live == nil || len(live.Photos) == 0 {
		return false
	}
	p = live
	sent := false
	for i, rel := range p.Photos {
		if i >= 3 {
			break
		}
		data, ok := readStoredPhoto(shopID, p, rel)
		if !ok {
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

// sessionPhotoRefs lists every stored photo path a live
// session still names: an open listing's kept photos and
// the burst batch and held sets. Registered with the
// catalogue at startup (see catalogue.ExtraPhotoRefs) so
// orphan deletion never takes a file a session can still
// send.
func sessionPhotoRefs() []string {
	var out []string
	sessions.rangeAll(func(_ string, s *Session) bool {
		if s == nil {
			return true
		}
		if s.Onboarding != nil {
			out = append(out, s.Onboarding.PhotoRefs...)
		}
		for _, it := range s.BurstBatch {
			out = append(out, it.PhotoRefs...)
		}
		for _, it := range s.BurstHeld {
			out = append(out, it.PhotoRefs...)
		}
		return true
	})
	return out
}

func init() {
	catalogue.ExtraPhotoRefs = sessionPhotoRefs
}
