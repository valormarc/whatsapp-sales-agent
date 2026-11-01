package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A buyer who names several products in one message means
// to pick them all: his live test named a crop top, a tote
// bag and a lingerie set in one breath and was shown only
// the crop top, so his "same for all" at the delivery
// question had nothing to hold on to. Typed picks now
// settle one by one, exactly as sheet picks do.
const typedPicksCatalogue = `{"merchants":{"efua":{"id":"efua","name":"Efua's Store","shopCode":"EFUA","ownerWhatsApp":"233550009001","payoutPhone":"233550009001","dailyGiveawayAllowance":300,"deliveryFee":20,"deliveryPaidTo":"merchant","products":[{"id":"crop-top","name":"Striped Crop Top","description":"White ribbed sleeveless crop top with black horizontal stripes.","listPrice":60,"floorPrice":45,"stock":2},{"id":"tote","name":"Black Tote Bag","description":"A roomy black tote.","listPrice":80,"floorPrice":60,"stock":2},{"id":"lingerie","name":"2 pieces lingerie set","description":"A two piece lingerie set.","listPrice":100,"floorPrice":80,"stock":2}]}}}`

func stubTypedPicksModel(t *testing.T) {
	t.Helper()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		text := ""
		for _, m := range body.Messages {
			text += m.Content + "\n"
		}
		her := text
		if i := strings.LastIndex(text, "Her message:"); i >= 0 {
			her = text[i:]
		}
		content := `{"intent":"unclear","reply":"Tell me more."}`
		if strings.Contains(strings.ToLower(her), "black totes bag") {
			content = `{"intent":"item","item":"Striped Crop Top","items":["Striped Crop Top","Black Tote Bag","2 pieces lingerie set"]}`
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		})
	}))
	t.Cleanup(stub.Close)
	t.Setenv("META_MODEL_API_KEY", "stub-key")
	t.Setenv("META_MODEL_API_BASE", stub.URL+"/v1")
}

func TestTypedPicksSettleOneByOne(t *testing.T) {
	dir := setupChat(t, "233550009001", typedPicksCatalogue)
	stubTypedPicksModel(t)
	c := &chat{dir: dir}
	buyer := "233550009002"

	c.say(t, buyer, "SHOP-EFUA")
	c.replies(t, buyer)
	c.say(t, buyer, "Strip crop top\nBlack totes bag\n1 lingerie")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "settle them one by one") {
		t.Fatalf("all three picks must be taken up together: %q", got)
	}
	if !strings.Contains(got, "Striped Crop Top - GH₵60") {
		t.Fatalf("the first pick goes in front with its price: %q", got)
	}
	s := sessions.get(sessionKey("efua", buyer))
	if s == nil || s.Product == nil || s.Product.ID != "crop-top" {
		t.Fatalf("the first pick stands in front: %+v", s)
	}
	if len(s.ShelfPicks) != 2 || s.ShelfPicks[0] != "tote" || s.ShelfPicks[1] != "lingerie" {
		t.Fatalf("the other two wait as picks, in her order: %v", s.ShelfPicks)
	}

	// Settling the first at its price brings the second up.
	c.say(t, buyer, "I will take it at 60")
	got = strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Black Tote Bag - GH₵80") {
		t.Fatalf("agreeing the first pick brings the next one up: %q", got)
	}
	s = sessions.get(sessionKey("efua", buyer))
	if s == nil || len(s.Basket) != 1 || len(s.ShelfPicks) != 1 || s.ShelfPicks[0] != "lingerie" {
		t.Fatalf("one settled, one still to come: basket %v picks %v", s.Basket, s.ShelfPicks)
	}
}
