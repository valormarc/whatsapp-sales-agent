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
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/datadir"
)

// Listing cutouts: a seller's photo is taken where her
// goods are — a crowded table, a busy stall, other stock
// behind them. When a photo is accepted into a listing,
// the segmentation service is asked for the product
// itself, cut out along its outline and set on a clean
// background, and that cutout becomes the picture buyers
// and sellers are shown for the product. The ask is made
// with the product's own name, the one the walk's reading
// settled on, so what gets cut is the thing she listed.
//
// The seller's original photo is always retained: before
// a cutout takes a photo's place, the untouched bytes are
// kept beside it under the -orig name, and the listing's
// record keeps pointing at the same path it always did,
// so every existing reader shows the cutout without
// knowing anything changed.
//
// The whole path is best effort by design. With no
// service configured, or on any failure along the way —
// the service unreachable, a slow cold start, no outline
// found, a reply that is not a picture — the listing
// keeps the original photo and nothing about the save
// changes. A listing never waits on, blocks on, or
// breaks because of a cutout.

// segmentEndpoint is the segmentation service's address,
// the single config value the feature hangs on. Empty
// means the feature is off and photos stay as taken.
func segmentEndpoint() string {
	return strings.TrimSpace(os.Getenv("SAM_SEGMENT_URL"))
}

// segmentCutoutFn is the seam tests stand in for: the real
// one speaks HTTP to the configured service.
var segmentCutoutFn = requestSegmentCutout

// cutoutGrace is how long a save waits for cutouts to
// land before it carries on: warm calls finish well
// inside it, so the announce and the buyer alerts that
// follow a save show the cutout, while a cold service
// never holds the conversation up — its cutouts still
// swap in whenever they arrive.
var cutoutGrace = 12 * time.Second

// cutoutCallTimeout bounds one segmentation call,
// background ones included. A service that scales up
// from cold takes its time; past this, the photo simply
// keeps its original.
var cutoutCallTimeout = 3 * time.Minute

// keptPhoto is one photo a save has just stored: where it
// lives, the bytes as taken, and the format they are in.
type keptPhoto struct {
	rel    string
	data   []byte
	format string
}

// applyCutouts asks the service for a cutout of each kept
// photo and swaps each one in as it lands. It returns
// once every cutout is in place or the grace has run
// out, whichever comes first; calls still in flight
// finish their swaps in the background.
func applyCutouts(product catalogue.Product, saved []keptPhoto) {
	if len(saved) == 0 || segmentEndpoint() == "" {
		return
	}
	prompt := strings.TrimSpace(product.Name)
	if prompt == "" {
		return
	}
	done := make(chan struct{}, len(saved))
	for _, kp := range saved {
		go func(kp keptPhoto) {
			defer func() { done <- struct{}{} }()
			ctx, cancel := context.WithTimeout(context.Background(), cutoutCallTimeout)
			defer cancel()
			cut, ok := segmentCutoutFn(ctx, kp.data, prompt, kp.format)
			if !ok || !plausibleCutout(cut, kp.format) {
				return
			}
			swapInCutout(kp, cut)
		}(kp)
	}
	timer := time.NewTimer(cutoutGrace)
	defer timer.Stop()
	for i := 0; i < len(saved); i++ {
		select {
		case <-done:
		case <-timer.C:
			return
		}
	}
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
