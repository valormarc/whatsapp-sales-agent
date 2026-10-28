package agent

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// The shop ID a same-name shop is given: name first, then a hyphen
// and a counting number. A second Belora Collection is told her
// shop code is BELORA-2; her name stays exactly what she gave, and
// buyers carrying her code land on her shelf and no one else's.
const oneBeloraCatalogue = `{"merchants":{` +
	`"belora":{"id":"belora","name":"Belora Collection","shopCode":"BELORA","ownerWhatsApp":"233550007001","payoutPhone":"233550007001","dailyGiveawayAllowance":100,"products":[{"id":"silk-scarf","name":"Silk Scarf","description":"A smooth silk scarf.","listPrice":200,"floorPrice":160,"stock":4}]}` +
	`}}`

func TestSecondSameNameShopGetsHyphenNumberCode(t *testing.T) {
	dir := setupChat(t, "233550009999", oneBeloraCatalogue)
	c := &chat{dir: dir}

	secondOwner := "233550007002"
	c.say(t, secondOwner, "I want to sell")
	c.last(t, secondOwner)
	c.say(t, secondOwner, "Belora Collection")
	joined := strings.Join(c.replies(t, secondOwner), "\n")
	if !strings.Contains(joined, "BELORA-2") {
		t.Fatalf("second same-name signup should hand her BELORA-2, got %q", joined)
	}
	shop := catalogue.MerchantByOwner(secondOwner)
	if shop == nil || shop.ShopCode != "BELORA-2" || shop.Name != "Belora Collection" {
		t.Fatalf("second shop = %+v", shop)
	}

	// She stocks a shelf of her own through the onboarding walk.
	c.say(t, secondOwner, "Ankara Headwrap")
	c.last(t, secondOwner)
	c.say(t, secondOwner, "Bright printed headwrap.")
	c.last(t, secondOwner)
	c.say(t, secondOwner, "90")
	c.last(t, secondOwner)
	c.say(t, secondOwner, "70")
	c.last(t, secondOwner)
	c.say(t, secondOwner, "only as a whole")
	c.last(t, secondOwner)
	c.say(t, secondOwner, "yes")
	if got := c.last(t, secondOwner); !strings.Contains(got, "Saved") {
		t.Fatalf("second shop product save = %q", got)
	}

	// Her link's entry text lands a buyer on her shelf only.
	buyer := "233550007101"
	c.say(t, buyer, "SHOP-BELORA-2")
	got := strings.Join(c.replies(t, buyer), "\n")
	if !strings.Contains(got, "Belora Collection") || !strings.Contains(got, "Ankara Headwrap") {
		t.Fatalf("SHOP-BELORA-2 entry = %q", got)
	}
	if strings.Contains(got, "Silk Scarf") {
		t.Fatalf("the second shop's greeting named the first shop's product: %q", got)
	}

	// The bare hyphen code, typed without the link, lands there too.
	buyer2 := "233550007102"
	c.say(t, buyer2, "belora-2")
	got = strings.Join(c.replies(t, buyer2), "\n")
	if !strings.Contains(got, "Ankara Headwrap") || strings.Contains(got, "Silk Scarf") {
		t.Fatalf("bare belora-2 entry = %q", got)
	}

	// The plain code still belongs to the first shop, untouched.
	buyer3 := "233550007103"
	c.say(t, buyer3, "SHOP-BELORA")
	got = strings.Join(c.replies(t, buyer3), "\n")
	if !strings.Contains(got, "Silk Scarf") || strings.Contains(got, "Ankara Headwrap") {
		t.Fatalf("SHOP-BELORA entry = %q", got)
	}
}
