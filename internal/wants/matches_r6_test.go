package wants

import "testing"

// Run-6 record: a want was consumed by an unrelated
// restock sharing a single word. A match now needs the
// want's words to cover the product name's significant
// words.
func TestMatchesWantNeedsTheWholeName(t *testing.T) {
	cases := []struct {
		want, product string
		match         bool
	}{
		{"the beaded bag", "Leather Bag", false},
		{"i need a beaded bag", "Beaded Bag", true},
		{"ankara fabric please", "Ankara Fabric", true},
		{"fabric for sewing", "Ankara Fabric", false},
		{"do you have a tv", "TV", true},
		{"handmade beaded bag with a gold clasp", "Handmade Beaded Bag", true},
		{"tilapia needed", "Smoked Tilapia", true},
		// A generic ask is met by a specific arrival: she
		// asked for a bag, and a bag came.
		{"the bag", "Beaded Bag", true},
	}
	for _, c := range cases {
		if got := MatchesWant(c.want, c.product); got != c.match {
			t.Fatalf("MatchesWant(%q, %q) = %v, want %v", c.want, c.product, got, c.match)
		}
	}
}
