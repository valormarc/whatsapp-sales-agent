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
	_ = os.MkdirAll(datadir.Path("photos"), 0o755)
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
