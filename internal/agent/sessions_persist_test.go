package agent

// Work in progress survives a restart: the session state
// is snapshotted, the in-memory maps are dropped the way a
// dying process drops them, the snapshot is restored the
// way a starting process restores it, and the conversation
// continues where it left off — a kept batch still priced
// by one reply, an open listing still finished by its own
// next answers.

import (
	"strings"
	"testing"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
)

// dropInMemoryState wipes the process's conversation state
// the way a restart does: the durable books (catalogue,
// orders, day book) live on disk and are untouched.
func dropInMemoryState() {
	sessions.replaceAll(map[string]*Session{})
	bindings.replaceAll(map[string]string{})
	doorShown.replaceAll(map[string]bool{})
	packageNames.replaceAll(map[string]string{})
}

func TestKeptBatchSurvivesRestart(t *testing.T) {
	stubPhotoMedia(t)
	owner := "233550006101"
	buyer := "233550006102"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, owner, "Corner Rack\nSteel frame with 4 tiers\nGH₵270")
	c.photo(t, owner, "Dinner Set\n16 pieces for the family\nGH₵180")
	c.replies(t, owner)
	c.say(t, owner, "250")
	c.replies(t, owner)
	c.say(t, owner, "whole")
	joined := strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(joined, "last price for each") || !strings.Contains(joined, "Dinner Set") {
		t.Fatalf("the batch should be presented before the restart, got %q", joined)
	}
	bindings.put(buyer, MerchantID)

	// The restart: snapshot, drop everything, restore.
	SnapshotSessionsNow()
	dropInMemoryState()
	if sessions.len() != 0 {
		t.Fatal("the wipe should empty the session state")
	}
	restoreSessions()

	s := sessions.get(sessionKey(MerchantID, owner))
	if s == nil || !s.BurstAwaiting || len(s.BurstBatch) != 1 {
		t.Fatalf("the kept batch should be restored mid-question, got %+v", s)
	}
	if bindings.get(buyer) != MerchantID {
		t.Fatalf("the buyer's binding should be restored, got %q", bindings.get(buyer))
	}

	// Her next message is still the batch's answer.
	c.say(t, owner, "170")
	joined = strings.Join(c.replies(t, owner), "\n")
	if !strings.Contains(joined, "Added ✅") || !strings.Contains(joined, "Dinner Set") {
		t.Fatalf("the restored batch should still be priced by one reply, got %q", joined)
	}
	dinner := catalogue.FindByName(MerchantID, "Dinner Set")
	if dinner == nil || dinner.ListPrice != 180 || dinner.FloorPrice != 170 {
		t.Fatalf("the dinner set should be saved at 180/170 after the restart, got %+v", dinner)
	}
	rack := catalogue.FindByName(MerchantID, "Corner Rack")
	if rack == nil || rack.ListPrice != 270 || rack.FloorPrice != 250 {
		t.Fatalf("the open listing should be saved at 270/250, got %+v", rack)
	}
}

func TestOpenListingSurvivesRestart(t *testing.T) {
	stubPhotoMedia(t)
	owner := "233550006201"
	dir := setupChat(t, owner, shopWithBag)
	c := &chat{dir: dir}

	c.photo(t, owner, "Corner Rack\nSteel frame with 4 tiers\nGH₵270")
	c.replies(t, owner)
	s := sessions.get(sessionKey(MerchantID, owner))
	if s == nil || s.Onboarding == nil {
		t.Fatal("a listing should be open before the restart")
	}

	SnapshotSessionsNow()
	dropInMemoryState()
	restoreSessions()

	s = sessions.get(sessionKey(MerchantID, owner))
	if s == nil || s.Onboarding == nil || s.Onboarding.Name != "Corner Rack" || s.Onboarding.ListPrice != 270 {
		t.Fatalf("the open listing should be restored with its draft, got %+v", s)
	}

	// The walk continues from its own next step.
	c.say(t, owner, "250")
	c.replies(t, owner)
	if catalogue.FindByName(MerchantID, "Corner Rack") == nil {
		c.say(t, owner, "whole")
		c.replies(t, owner)
	}
	rack := catalogue.FindByName(MerchantID, "Corner Rack")
	if rack == nil || rack.ListPrice != 270 || rack.FloorPrice != 250 || len(rack.Photos) != 1 {
		t.Fatalf("the restored listing should save at 270/250 with its photo, got %+v", rack)
	}
}
