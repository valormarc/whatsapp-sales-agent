// The signals a buyer's words carry inside a shop, and the
// lane: the agent sells for one shop, and turns that are clearly
// about something else are steered back, then left alone.
package agent

import (
	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
	"regexp"
	"strings"
)

var (
	linkRe     = regexp.MustCompile(`(link|pay|checkout)`)
	yesRe      = regexp.MustCompile(`(yes|ok|okay|deal|send|take|agree)`)
	howMuchRe  = regexp.MustCompile(`(how much|price)`)
	intentRe   = regexp.MustCompile(`(how much|price|cost|offer|buy|interested|need|want|looking for|get me|sell me|give me)`)
	greetingRe = regexp.MustCompile(`^(hi|hello|hey|good (morning|afternoon|evening))\b`)
	// wantRe catches the bare want, the way buyers open in the real
	// selling groups: "tv needed", "I need one", "who sells bags".
	wantRe = regexp.MustCompile(`(needed|need|want|looking for|searching for|who sells|who has|any plug|a plug|plug for)`)
	// picRe catches the buyer's other first move: "send pic".
	picRe = regexp.MustCompile(`(\bpic\b|pics|photo|picture|image)`)
	// buyIntentRe catches commitment words with no price in them:
	// "I'll buy", "I'll take it", "I want to buy". Warm words, but
	// they are not an order; they open the explicit confirmation
	// step, and only a clear yes there closes.
	buyIntentRe = regexp.MustCompile(`(i['’]ll buy|i will buy|i['’]ll take|i will take|i want to buy|\btake it\b|\bbuy it\b)`)
	// deliveryRe catches the delivery question, which real buyers
	// ask before the item price as often as after it.
	deliveryRe = regexp.MustCompile(`(deliver|delivery|how do i get it|how will i receive|how do i receive|how will i get it|transport|\bbus\b)`)
	// totalRe catches the demand for one figure: "How much in
	// total?" A buyer should never have to add two numbers herself.
	totalRe = regexp.MustCompile(`\btotal\b`)
	// needRe catches the need-led opening: the buyer describes a
	// problem, not a product ("something to keep the moisture",
	// "something for my hair"), and lets the seller recommend.
	needRe = regexp.MustCompile(`(something (to|for|that)|need something|anything (to|for)|for my (hair|skin|face|body))`)
	// The availability frames: the standard buyer ask in this
	// market is a question, "do you have X", "have you got X",
	// "do you sell X", "is X available". None of them carries a
	// want word for wantRe to catch, so the asked-for thing is
	// pulled out of the frame itself (see unstockedAsk).
	doYouHaveRe   = regexp.MustCompile(`^do you have (?:got )?(.+)$`)
	haveYouGotRe  = regexp.MustCompile(`^have you got (.+)$`)
	doYouSellRe   = regexp.MustCompile(`^(?:do you sell|are you selling) (.+)$`)
	isAvailableRe = regexp.MustCompile(`^is (.+?) available$`)
	// The lane patterns: clear off-topic asks, the ones people put
	// to any AI they can reach — meanings, sums, public figures,
	// capitals, homework, jokes, the weather. Narrow on purpose,
	// and the lane block only ever tries them on a message with no
	// shop signal in it at all, so nothing the shop can answer is
	// ever steered away from the shop.
	arithmeticRe = regexp.MustCompile(`\d+\s*[-+*/x]\s*\d+`)
	offTopicRe   = regexp.MustCompile(`(who is|who was|who won|what is the meaning|what does .+ mean|meaning of|capital of|president of|prime minister of|how old is|tell me a joke|write (me )?(an? )?(essay|poem|story|letter|cv|code|program)|translate .+ (to|into)|what is the weather|weather (today|now)|news today)`)
)

// availabilityFramed reports whether the buyer asked in the
// availability frame ("is X available", "do you have X"): in the
// real groups a seller answers that question with the plain words
// "Still available", and the agent leads with them too.
func availabilityFramed(lower string) bool {
	text := strings.TrimRight(strings.TrimSpace(lower), ".?!")
	return isAvailableRe.MatchString(text) || doYouHaveRe.MatchString(text) ||
		haveYouGotRe.MatchString(text) || doYouSellRe.MatchString(text)
}

// laneStrike answers one clear off-topic turn, in the shape the
// merchant's money deserves: the first turn earns one short steer
// naming the agent's work, with the shelf offered; the second a
// briefer repeat; the third closes the lane politely. After that
// the buyer's off-topic turns are met with silence (see the lane
// block in HandleInbound) until she talks about the shop again.
// Nothing here lectures and nothing answers the question itself:
// answering even the easy ones teaches the whole market to come
// back with more, and every answer is bought with the merchant's
// money and spoken in her name.
func laneStrike(from string, s *Session, merchant *catalogue.Merchant) {
	s.OffTopic++
	switch {
	case s.OffTopic == 1:
		sendPieces(from,
			"That one is outside my work here. I sell for "+merchant.Name+": the goods, their prices and delivery.",
			"Tell me what you are after and I will sort you out.\n"+productListText(merchant.ID))
	case s.OffTopic == 2:
		whatsapp.SendText(from, "I only handle the shop here: what we sell, prices and delivery. Name an item and I will price it for you.")
	default:
		s.LaneClosed = true
		whatsapp.SendText(from, "I will be here when you need something from the shop.")
	}
}
