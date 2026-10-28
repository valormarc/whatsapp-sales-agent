package catalogue

import (
	"os"
	"path/filepath"
	"testing"
)

// emptyCatalogue points the store at a catalogue file holding no
// shops at all, so shop creation starts from a clean book.
func emptyCatalogue(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HACKATHON_AGENT_DATA_DIR", dir)
	b, err := marshalJSON(Catalogue{Merchants: map[string]Merchant{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "catalogue.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Three sellers open shops under the exact same name. The name is
// never touched; what separates them is the shop code, and a clash
// is settled with a hyphen and a counting number (BELORA,
// BELORA-2, BELORA-3), never a number melted into the name
// (BELORA2), which reads as if the shop itself were named that.
func TestShopCodesForRepeatedNames(t *testing.T) {
	emptyCatalogue(t)

	owners := []string{"233550006001", "233550006002", "233550006003"}
	wantCodes := []string{"BELORA", "BELORA-2", "BELORA-3"}
	ids := map[string]bool{}
	for i, owner := range owners {
		created, err := CreateMerchant("Belora Collection", owner)
		if err != nil {
			t.Fatal(err)
		}
		if created.ShopCode != wantCodes[i] {
			t.Fatalf("shop %d code = %q, want %q", i+1, created.ShopCode, wantCodes[i])
		}
		if created.Name != "Belora Collection" {
			t.Fatalf("shop %d name = %q, the name she gave must be kept whole", i+1, created.Name)
		}
		if ids[created.ID] {
			t.Fatalf("shop %d reused id %q", i+1, created.ID)
		}
		ids[created.ID] = true
		// Each code resolves back to its own shop and its own owner.
		if m := MerchantByShopCode(created.ShopCode); m == nil || m.OwnerWhatsApp != owner {
			t.Fatalf("code %q does not resolve to its owner: %+v", created.ShopCode, m)
		}
	}
	// The first shop's code never moved when the later ones arrived.
	if m := MerchantByShopCode("BELORA"); m == nil || m.OwnerWhatsApp != owners[0] {
		t.Fatalf("first shop disturbed by later signups: %+v", m)
	}
	if m := MerchantByShopCode("belora-2"); m == nil || m.OwnerWhatsApp != owners[1] {
		t.Fatalf("lower-case hyphen code lookup failed: %+v", m)
	}
}
