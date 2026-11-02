package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
)

// Listing cutouts: a seller's photo is taken where her
// goods are — a crowded table, a busy stall, other stock
// behind them. The segmentation service can cut the
// product itself out along its outline and set it on a
// clean background, and that cutout can take the photo's
// place so buyers and sellers see the goods clean. The
// ask is made with the product's own name, the one the
// walk's reading settled on, so what gets cut is the
// thing she listed.
//
// The edit is hers to instruct, never the platform's to
// assume (her ruling, 2 November): not everything in a
// photo is background — the bowls were photographed with
// their box as part of the goods, and a dress on the
// woman wearing it sells as it is worn — so a photo is
// stored and shown exactly as she sent it until she says
// yes to the edit Stall offers, or asks for it in her own
// words (see cutoutoffer.go). No instruction, no cutout.
//
// The seller's original photo is always retained: before
// a cutout takes a photo's place, the untouched bytes are
// kept beside it under the -orig name, and the listing's
// record keeps pointing at the same path it always did,
// so every existing reader shows the cutout without
// knowing anything changed.
//
// The road itself is best effort by design. With no
// service configured, or on any failure along the way —
// the service unreachable, a slow cold start, no outline
// found, a reply that is not a picture — the listing
// keeps the original photo, and she is told plainly the
// edit could not be done.

// segmentEndpoint is the segmentation service's address,
// the single config value the feature hangs on. Empty
// means the feature is off and photos stay as taken.
func segmentEndpoint() string {
	return strings.TrimSpace(os.Getenv("SAM_SEGMENT_URL"))
}

// segmentCutoutFn is the seam tests stand in for: the real
// one speaks HTTP to the configured service.
var segmentCutoutFn = requestSegmentCutout

// cutoutCallTimeout bounds one segmentation call. A
// service that scales up from cold takes its time; past
// this, the photo simply keeps its original.
var cutoutCallTimeout = 3 * time.Minute

// keptPhoto is one photo a save has just stored: where it
// lives, the bytes as taken, and the format they are in.
type keptPhoto struct {
	rel    string
	data   []byte
	format string
}

// cutOutProductPhotos runs the instructed edit over
// every photo a product holds: each stored photo goes to
// the service under the product's own listed name, and
// each cutout that comes back sound takes its photo's
// place, the original kept beside it. It reports how
// many photos were cut out of how many the product
// holds. It is only ever called on her instruction —
// her yes to the offer, or her own words (see
// cutoutoffer.go) — never at acceptance.
func cutOutProductPhotos(shopID string, p *catalogue.Product) (cut, total int) {
	if p == nil || segmentEndpoint() == "" {
		return 0, 0
	}
	prompt := strings.TrimSpace(p.Name)
	if prompt == "" {
		return 0, 0
	}
	total = len(p.Photos)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, rel := range p.Photos {
		data, ok := readStoredPhoto(shopID, p, rel)
		if !ok {
			continue
		}
		format := "jpeg"
		switch {
		case strings.HasSuffix(rel, ".png"):
			format = "png"
		case strings.HasSuffix(rel, ".webp"):
			format = "webp"
		}
		wg.Add(1)
		go func(rel string, data []byte, format string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), cutoutCallTimeout)
			defer cancel()
			cutBytes, ok := segmentCutoutFn(ctx, data, prompt, format)
			if !ok || !plausibleCutout(cutBytes, format) {
				return
			}
			swapInCutout(keptPhoto{rel: rel, data: data, format: format}, cutBytes)
			mu.Lock()
			cut++
			mu.Unlock()
		}(rel, data, format)
	}
	wg.Wait()
	return cut, total
}

// swapInCutout puts a cutout in a kept photo's place. The
// original bytes are preserved first, beside the photo
// under the -orig name, and the cutout is moved in
// atomically, so a reader of the photo sees either the
// whole original or the whole cutout, never a half
// written file. Any failure leaves the original in place.
func swapInCutout(kp keptPhoto, cut []byte) {
	origRel := originalPhotoRel(kp.rel)
	origPath := datadir.Path(origRel)
	if _, err := os.Stat(origPath); os.IsNotExist(err) {
		if err := os.WriteFile(origPath, kp.data, 0o600); err != nil {
			return
		}
	}
	tmpRel := kp.rel + ".cuttmp"
	tmpPath := datadir.Path(tmpRel)
	if err := os.WriteFile(tmpPath, cut, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmpPath, datadir.Path(kp.rel)); err != nil {
		_ = os.Remove(tmpPath)
	}
}

// originalPhotoRel is where a photo's untouched original
// is kept: the same name with -orig before the extension.
// The listing's record never names it, so it is never
// sent or counted as one of the product's photos; it is
// simply retained.
func originalPhotoRel(rel string) string {
	ext := filepath.Ext(rel)
	return strings.TrimSuffix(rel, ext) + "-orig" + ext
}

// plausibleCutout is the cheap truth check on a service
// reply: a real composite is never a handful of bytes,
// and for the formats the standard library can read, its
// header must actually decode as a picture of the format
// that was asked for. WebP has no standard reader, so
// there the container's own magic stands in.
func plausibleCutout(data []byte, format string) bool {
	if len(data) < 512 {
		return false
	}
	switch format {
	case "jpeg", "png":
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		return err == nil && cfg.Width > 0 && cfg.Height > 0
	case "webp":
		return len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
	}
	return true
}

// requestSegmentCutout is the real call: the photo and
// the product's name go to the configured service, and
// the cutout it answers with comes back, composited in
// the photo's own format so the file a listing points at
// never claims to be one picture type while holding
// another. The reply is either the cutout image itself
// or a JSON body carrying it base64 encoded.
func requestSegmentCutout(ctx context.Context, photo []byte, prompt, format string) ([]byte, bool) {
	endpoint := segmentEndpoint()
	if endpoint == "" || len(photo) == 0 {
		return nil, false
	}
	body, err := json.Marshal(map[string]string{
		"image_b64": base64.StdEncoding.EncodeToString(photo),
		"prompt":    prompt,
		"format":    format,
	})
	if err != nil {
		return nil, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(os.Getenv("SAM_SEGMENT_KEY")); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil || len(data) == 0 {
		return nil, false
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		return data, true
	}
	var reply struct {
		CutoutB64 string `json:"cutout_b64"`
	}
	if err := json.Unmarshal(data, &reply); err != nil || reply.CutoutB64 == "" {
		return nil, false
	}
	cut, err := base64.StdEncoding.DecodeString(reply.CutoutB64)
	if err != nil || len(cut) == 0 {
		return nil, false
	}
	return cut, true
}
