package agent

import (
	"testing"
)

// A redelivered message is handled once. WhatsApp delivers
// a webhook again when the answer is slow, and the second
// delivery must not answer, collect or save anything: the
// same photo must not open a second listing, and the same
// text must not answer the next listing's question.
func TestRedeliveredMessageIsHandledOnce(t *testing.T) {
	owner := "233550002501"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	HandleInbound(Inbound{From: owner, MessageID: "wamid-dup-photo", ImageID: "dup-img-1", Caption: "Corner Rack GH₵270"})
	first := c.replies(t, owner)
	if len(first) == 0 {
		t.Fatal("the first delivery should be answered")
	}
	HandleInbound(Inbound{From: owner, MessageID: "wamid-dup-photo", ImageID: "dup-img-1", Caption: "Corner Rack GH₵270"})
	if got := c.replies(t, owner); len(got) != 0 {
		t.Fatalf("the redelivery should be silent, got %q", got)
	}
	s := sessions[sessionKey(MerchantID, owner)]
	if s == nil || s.Onboarding == nil || len(s.Onboarding.PhotoRefs) != 1 {
		t.Fatalf("the redelivery must not touch the open listing: %+v", s)
	}

	// A message with no id cannot be judged and is handled.
	HandleInbound(Inbound{From: owner, Text: "hello"})
	_ = c.replies(t, owner)
}

// The tightened name judgement: containment is the same
// product, a shared category word is not.
func TestSameProductNameContainment(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Maxi Dress", "White Maxi Dress", true},
		{"B Buckle Slides", "Buckle Slides", true},
		{"Thong Sandals", "Flat strappy sandals", false},
		{"Studded thong slippers", "Thong Sandals", false},
		{"Heels", "Heels", true},
		{"Ankara Fabric", "Kente Cloth", false},
		{"Rack", "Bathroom Corner Rack Set", false},
		{"Slides", "B Buckle Slides", false},
		{"Dress", "Dress", true},
	}
	for _, tc := range cases {
		if got := sameProductName(tc.a, tc.b); got != tc.want {
			t.Fatalf("sameProductName(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
