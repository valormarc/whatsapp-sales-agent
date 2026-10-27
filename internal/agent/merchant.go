package agent

// Merchant onboarding and catalogue commands, inside the same chat
// customers haggle in. One door, two modes: only the merchant's own
// number (see IsMerchant) ever reaches this file's handlers.
//
// The onboarding flow is a small deterministic state machine held in
// her session. No model calls take part in it: a product's name,
// prices and floor are collected question by question, and nothing is
// saved until she confirms the summary with a clear yes. The floor is
// explained plainly when it is asked for, and it is never shown to
// customers: the merchant's own catalogue listing is the only place
// floors are ever printed, and only her number can ask for it.

import (
	"regexp"
	"strings"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/wants"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

// OnboardingFlow is one in-progress "add a product" walk. Step is one
// of: name, description, list, floor, measure, measureqty, confirm.
// The measure steps sit between the floor and the summary: whether
// she sells the product by measure, and if so how much of it the
// list price she just gave is for.
type OnboardingFlow struct {
	Step        string
	Name        string
	Description string
	ListPrice   float64
	FloorPrice  float64
	Unit        string
	BaseQty     float64
}

var (
	addProductRe   = regexp.MustCompile(`\b(add|new)\s+(a\s+|an\s+|another\s+)?(new\s+)?(product|item|goods?|stock)\b`)
	myCatalogueRe  = regexp.MustCompile(`\bmy (catalogue|products|items)\b`)
	changePriceRe  = regexp.MustCompile(`^change (?:the )?(?:list )?price of (.+?) to (.+)$`)
	changeFloorRe  = regexp.MustCompile(`^change (?:the )?floor(?: price)? of (.+?) to (.+)$`)
	removeRe       = regexp.MustCompile(`^remove (?:the )?(.+)$`)
	setAllowanceRe = regexp.MustCompile(`\b(set|change|update|make)\b[^0-9]*\ballowance\b|\ballowance\b[^0-9]*\d`)
	// Delivery commands: the fee parses like any price; the note is
	// captured from her original words, case and all, because buyers
	// will read it back exactly as she wrote it.
	setDeliveryFeeRe  = regexp.MustCompile(`^set delivery fee (.+)$`)
	setDeliveryNoteRe = regexp.MustCompile(`(?i)^set delivery note (.+)$`)
	// Measure commands: a product gains a measure ("set measure of
	// <item> to <qty> <unit>") or loses it ("sell <item> whole
	// only", "<item> whole only"), in her own words.
	setMeasureRe = regexp.MustCompile(`^set measure of (.+?) to (.+)$`)
	wholeOnlyRe  = regexp.MustCompile(`^(?:sell )?(.+?) whole only$`)
	// The wants list: what her buyers asked for that she does not
	// stock, kept for her in the want book.
	wantsListRe = regexp.MustCompile(`^(buyer )?wants$|^(buyer )?requests$`)
	// Who takes the delivery money is her decision, in her own
	// plain shapes: the money comes to her, inside her buyers'
	// payment, or her buyers pay whoever brings the item.
	deliveryToMeRe      = regexp.MustCompile(`(delivery money comes to me|i take the delivery money|delivery money to me|i collect the delivery money|delivery money is mine)`)
	deliveryToCarrierRe = regexp.MustCompile(`(buyer pays the delivery|buyer pays delivery|buyers pay the delivery|buyer pays the rider|buyer pays the carrier|delivery money goes to the (rider|carrier|driver|station))`)
)

// controlWord normalises a short reply for comparison: lower case,
// trimmed, with trailing punctuation dropped, so "Yes." and "cancel!"
// read as the words she means.
func controlWord(lower string) string {
	return strings.Trim(lower, " .!")
}

func isClearYes(lower string) bool {
	switch controlWord(lower) {
	case "yes", "yeah", "yep", "yes please", "confirm", "save", "save it", "ok", "okay", "go ahead":
		return true
	}
	return false
}

func isClearNo(lower string) bool {
	switch controlWord(lower) {
	case "no", "nope", "no thanks", "drop it", "do not", "don't":
		return true
	}
	return false
}

// handleMerchantMessage is the merchant-mode dispatcher for one
// shop: the shop the sender owns, resolved by the caller before
// anything else, and the only shop her words can touch. Order
// matters: an in-progress flow owns her words; a pending removal
// owns her next reply; then the onboarding trigger; then, on an
// empty shelf, the first-run greeting; then catalogue commands; and
// only then her day questions, answered from the day book as before.
func handleMerchantMessage(from, text, merchantID string) {
	merchant := catalogue.GetMerchant(merchantID)
	if merchant == nil {
		return
	}
	lower := strings.ToLower(text)
	key := sessionKey(merchantID, from)
	s, ok := sessions[key]
	if !ok {
		s = &Session{Stage: "merchant", MerchantID: merchantID}
		sessions[key] = s
	}

	if s.Onboarding != nil {
		advanceOnboarding(from, s, text, lower)
		return
	}

	if s.PendingRemove != "" {
		name := s.PendingRemove
		s.PendingRemove = ""
		if isClearYes(lower) {
			removed, err := catalogue.RemoveProduct(merchantID, name)
			if err != nil {
				whatsapp.SendText(from, "I could not remove it after all: "+err.Error()+". Nothing changed.")
				return
			}
			whatsapp.SendText(from, removed.Name+" is out of your catalogue. Customers will no longer be offered it.")
			return
		}
		whatsapp.SendText(from, "No problem. "+name+" stays in your catalogue.")
		return
	}

	// Her word on an open direct-payment claim settles it (see
	// payment.go). It sits after her in-progress flows, which own
	// her words while they are open, and before her commands and
	// day questions. With no open claim in her shop it consumes
	// nothing and her message falls through untouched.
	if handleMerchantClaim(from, merchant, lower) {
		return
	}

	if addProductRe.MatchString(lower) {
		s.Onboarding = &OnboardingFlow{Step: "name"}
		whatsapp.SendText(from, "Let us add a new product. What is it called? You can say cancel at any point and nothing will be saved.")
		return
	}

	if len(merchant.Products) == 0 {
		whatsapp.SendText(from, "Hello. Your shelf at "+merchant.Name+" is empty just now, so there is nothing to sell yet. Say \"add a product\" and I will walk you through it: a name, a short description, the list price customers see, and your floor price, the lowest you would accept, which customers never see. The moment you confirm, I can sell it.")
		return
	}

	if handleMerchantCommand(from, s, merchant, text, lower) {
		return
	}

	answerMerchantDayQuestion(from, text, merchantID)
}

// advanceOnboarding feeds one merchant message to the onboarding flow.
func advanceOnboarding(from string, s *Session, text, lower string) {
	flow := s.Onboarding

	if controlWord(lower) == "cancel" {
		s.Onboarding = nil
		whatsapp.SendText(from, "Dropped it. Nothing was saved. Say \"add a product\" whenever you are ready.")
		return
	}

	switch flow.Step {
	case "name":
		name := strings.TrimSpace(text)
		if name == "" {
			whatsapp.SendText(from, "What is the product called?")
			return
		}
		if catalogue.FindByName(s.MerchantID, name) != nil {
			whatsapp.SendText(from, "You already sell something called "+name+". Give this one a different name, or say cancel to stop.")
			return
		}
		flow.Name = name
		flow.Step = "description"
		whatsapp.SendText(from, "Tell me about "+name+" in a few words: what it is, and what makes it worth buying. Customers will read this.")

	case "description":
		description := strings.TrimSpace(text)
		if description == "" {
			whatsapp.SendText(from, "Give me a short description for "+flow.Name+": a line or two customers will read.")
			return
		}
		flow.Description = description
		flow.Step = "list"
		whatsapp.SendText(from, "What is the list price for "+flow.Name+"? This is the price customers see first. Give it in cedis, for example 450.")

	case "list":
		amount := parseOffer(lower)
		if amount == nil || *amount <= 0 {
			whatsapp.SendText(from, "I could not read that as a price. What is the list price for "+flow.Name+", in cedis? For example, "+money.GHS(450)+".")
			return
		}
		flow.ListPrice = *amount
		flow.Step = "floor"
		whatsapp.SendText(from, "Now your floor price for "+flow.Name+": the lowest you would accept for it. Customers never see this, and I will never sell below it. What is your floor?")

	case "floor":
		amount := parseOffer(lower)
		if amount == nil || *amount <= 0 {
			whatsapp.SendText(from, "I could not read that as a price. What is the lowest you would accept for "+flow.Name+", in cedis?")
			return
		}
		if *amount >= flow.ListPrice {
			whatsapp.SendText(from, "That floor will not work: it has to sit below the list price of "+money.GHS(flow.ListPrice)+", because the gap between the two is the room I have to haggle in. If the floor were the price itself, there would be nothing to negotiate. What floor below "+money.GHS(flow.ListPrice)+" suits you?")
			return
		}
		flow.FloorPrice = *amount
		flow.Step = "measure"
		whatsapp.SendText(from, "Do you sell the "+flow.Name+" by measure, like by the yard, litre or kilo, or only as a whole?")

	case "measure":
		// The answer can carry everything at once ("by the yard,
		// 6 yards"): a quantity phrase settles both the unit and
		// how much of it her list price is for.
		if q, ok := parseQuantity(lower); ok {
			flow.Unit, flow.BaseQty = q.Unit, q.Qty
			enterOnboardingConfirm(from, flow)
			return
		}
		if strings.Contains(lower, "whole") || isClearNo(lower) {
			enterOnboardingConfirm(from, flow)
			return
		}
		if unit, ok := unitMentioned(lower); ok {
			flow.Unit = unit
			flow.Step = "measureqty"
			whatsapp.SendText(from, "How much of it is the "+money.GHS(flow.ListPrice)+" price for?")
			return
		}
		whatsapp.SendText(from, "I did not catch that. Do you sell the "+flow.Name+" by measure, like by the yard, litre or kilo, or only as a whole?")

	case "measureqty":
		if q, ok := parseQuantity(lower); ok {
			flow.Unit, flow.BaseQty = q.Unit, q.Qty
			enterOnboardingConfirm(from, flow)
			return
		}
		if amount := parseOffer(lower); amount != nil && *amount > 0 {
			// A bare number takes the unit she already named.
			flow.BaseQty = *amount
			enterOnboardingConfirm(from, flow)
			return
		}
		whatsapp.SendText(from, "How much of it is the "+money.GHS(flow.ListPrice)+" price for? Give it like 6 "+unitWord(flow.Unit, 2)+".")

	case "confirm":
		switch {
		case isClearYes(lower):
			product, err := catalogue.AddProduct(s.MerchantID, catalogue.Product{
				Name:        flow.Name,
				Description: flow.Description,
				ListPrice:   flow.ListPrice,
				FloorPrice:  flow.FloorPrice,
				Stock:       1,
				Unit:        flow.Unit,
				BaseQty:     flow.BaseQty,
			})
			s.Onboarding = nil
			if err != nil {
				whatsapp.SendText(from, "I could not save it: "+err.Error()+". Nothing went live.")
				return
			}
			whatsapp.SendText(from, "Saved. "+product.Name+" is live at "+money.GHS(product.ListPrice)+", and I can sell it straight away. Your floor of "+money.GHS(product.FloorPrice)+" stays between us.")
			// Buyers who asked for this before it existed are told
			// now, in this moment, not left waiting on an alert
			// that never comes.
			alertWaitingBuyers(s.MerchantID, product)
		case isClearNo(lower):
			s.Onboarding = nil
			whatsapp.SendText(from, "Dropped it. Nothing was saved. Say \"add a product\" whenever you are ready.")
		default:
			whatsapp.SendText(from, "Nothing is saved until you say yes. Shall I add "+flow.Name+" at "+money.GHS(flow.ListPrice)+"? Say yes to save it, or no to drop it.")
		}
	}
}

// enterOnboardingConfirm moves the add-a-product walk to its
// summary: everything she has given, the measure in plain words
// when she sells by one, and the margin she is handing the agent
// to haggle with. Nothing is saved until her clear yes at the
// confirm step, exactly as before measures existed.
func enterOnboardingConfirm(from string, flow *OnboardingFlow) {
	flow.Step = "confirm"
	summary := "Here is what will go live:\n" +
		flow.Name + ": " + flow.Description + "\n" +
		"List price " + money.GHS(flow.ListPrice) + ", your floor " + money.GHS(flow.FloorPrice) + " (customers never see the floor).\n"
	if flow.Unit != "" && flow.BaseQty > 0 {
		probe := catalogue.Product{ListPrice: flow.ListPrice, FloorPrice: flow.FloorPrice, Unit: flow.Unit, BaseQty: flow.BaseQty}
		summary += "Sold by the " + flow.Unit + ": the " + money.GHS(flow.ListPrice) + " price is for " + formatQty(flow.BaseQty) + " " + unitWord(flow.Unit, flow.BaseQty) + ", so " + money.GHS(probe.UnitPrice()) + " per " + flow.Unit + ", and buyers can take less.\n"
	}
	summary += "That leaves " + money.GHS(flow.ListPrice-flow.FloorPrice) + " of margin I can give away in haggling, always inside your daily allowance.\n" +
		"Shall I add it? Say yes to save it, or no to drop it."
	whatsapp.SendText(from, summary)
}

// handleMerchantCommand runs one catalogue command outside the
// onboarding flow. It returns true when the message was a command,
// whether it succeeded or not, so the caller does not fall through to
// the day-question path.
func handleMerchantCommand(from string, s *Session, merchant *catalogue.Merchant, text, lower string) bool {
	if myCatalogueRe.MatchString(lower) {
		lines := []string{"Here is your catalogue:"}
		for _, p := range merchant.Products {
			line := "- " + p.Name + ": list " + money.GHS(p.ListPrice)
			if p.ByMeasure() {
				line += " (" + money.GHS(p.UnitPrice()) + " per " + p.Unit + ", sold by the " + p.Unit + ")"
			}
			line += ", your floor " + money.GHS(p.FloorPrice)
			lines = append(lines, line)
		}
		lines = append(lines, "Your daily giveaway allowance is "+money.GHS(merchant.DailyGiveawayAllowance)+". Floors are yours only; customers never see them.")
		if merchant.DeliveryFee > 0 || merchant.DeliveryNote != "" {
			line := "Delivery:"
			if merchant.DeliveryFee > 0 {
				line += " " + money.GHS(merchant.DeliveryFee) + " a trip."
			}
			if merchant.DeliveryNote != "" {
				line += " " + merchant.DeliveryNote
			}
			lines = append(lines, line)
			if deliveryPaidToOf(merchant) == "merchant" {
				lines = append(lines, "The delivery money comes to you, inside the total your buyers pay here.")
			} else {
				lines = append(lines, "The delivery money is not yours: your buyers pay whoever brings the item, when it arrives.")
			}
		} else {
			lines = append(lines, "No delivery fee set yet. Say \"set delivery fee 25\" to give buyers one total to pay.")
		}
		whatsapp.SendText(from, strings.Join(lines, "\n"))
		return true
	}

	if m := setDeliveryFeeRe.FindStringSubmatch(lower); m != nil {
		amount := parseOffer(m[1])
		if amount == nil {
			whatsapp.SendText(from, "I could not read the fee in that. Try: set delivery fee 25.")
			return true
		}
		if err := catalogue.SetDeliveryFee(merchant.ID, *amount); err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		if *amount == 0 {
			whatsapp.SendText(from, "Done. There is no delivery fee now; buyers pay just the item price, and delivery is arranged with you.")
		} else {
			whatsapp.SendText(from, "Done. Delivery now costs your buyers "+money.GHS(*amount)+" a trip, on top of the item price. They see one total before they pay.")
		}
		return true
	}

	if deliveryToMeRe.MatchString(lower) {
		if err := catalogue.SetDeliveryPaidTo(merchant.ID, "merchant"); err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. The delivery money now comes to you: it sits inside the one total your buyers pay here, and I will tell them so.")
		return true
	}

	if deliveryToCarrierRe.MatchString(lower) {
		if err := catalogue.SetDeliveryPaidTo(merchant.ID, "carrier"); err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. Your buyers now pay the delivery money to whoever brings the item, when it arrives. What comes to you here is the item money only.")
		return true
	}

	if m := setDeliveryNoteRe.FindStringSubmatch(text); m != nil {
		note := strings.TrimSpace(m[1])
		if err := catalogue.SetDeliveryNote(merchant.ID, note); err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. When buyers ask about delivery, they will hear: "+note)
		return true
	}

	if wantsListRe.MatchString(controlWord(lower)) {
		list := wants.Default.List(merchant.ID)
		if len(list) == 0 {
			whatsapp.SendText(from, "No buyer wants written down yet. When a buyer asks for something you do not stock, I keep it here for you.")
			return true
		}
		lines := []string{"Here is what buyers have asked for that you do not stock:"}
		for _, w := range list {
			line := "- \"" + w.Text + "\" from " + w.Buyer + " (" + w.Date + ")"
			if w.Alerted {
				line += " - told them when it landed"
			}
			lines = append(lines, line)
		}
		lines = append(lines, "Stock any of these and I will alert the buyer who asked, there and then.")
		whatsapp.SendText(from, strings.Join(lines, "\n"))
		return true
	}

	if m := changePriceRe.FindStringSubmatch(lower); m != nil {
		amount := parseOffer(m[2])
		if amount == nil {
			whatsapp.SendText(from, "I could not read the new price in that. Try: change price of <name> to 500.")
			return true
		}
		product, err := catalogue.UpdateListPrice(merchant.ID, strings.TrimSpace(m[1]), *amount)
		if err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. "+product.Name+" now lists at "+money.GHS(product.ListPrice)+". Its floor stays "+money.GHS(product.FloorPrice)+".")
		return true
	}

	if m := changeFloorRe.FindStringSubmatch(lower); m != nil {
		amount := parseOffer(m[2])
		if amount == nil {
			whatsapp.SendText(from, "I could not read the new floor in that. Try: change floor of <name> to 500.")
			return true
		}
		product, err := catalogue.UpdateFloorPrice(merchant.ID, strings.TrimSpace(m[1]), *amount)
		if err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. The floor for "+product.Name+" is now "+money.GHS(product.FloorPrice)+". Its list price stays "+money.GHS(product.ListPrice)+".")
		return true
	}

	if m := setMeasureRe.FindStringSubmatch(lower); m != nil {
		q, ok := parseQuantity(m[2])
		if !ok {
			whatsapp.SendText(from, "I could not read the measure in that. Try: set measure of <name> to 6 yards.")
			return true
		}
		product, err := catalogue.SetMeasure(merchant.ID, strings.TrimSpace(m[1]), q.Unit, q.Qty)
		if err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. "+product.Name+" is now sold by the "+product.Unit+": the "+money.GHS(product.ListPrice)+" price is for "+formatQty(product.BaseQty)+" "+unitWord(product.Unit, product.BaseQty)+", so "+money.GHS(product.UnitPrice())+" per "+product.Unit+". Buyers can take less, priced from the same rate.")
		return true
	}

	if m := wholeOnlyRe.FindStringSubmatch(lower); m != nil {
		name := strings.TrimSpace(m[1])
		product := catalogue.FindByName(merchant.ID, name)
		if product == nil {
			whatsapp.SendText(from, "I cannot find \""+name+"\" in your catalogue. Say \"my catalogue\" to see what is there.")
			return true
		}
		updated, err := catalogue.ClearMeasure(merchant.ID, product.Name)
		if err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. "+updated.Name+" is sold whole only now: "+money.GHS(updated.ListPrice)+" as it comes, and I will not split it.")
		return true
	}

	if setAllowanceRe.MatchString(lower) {
		amount := parseOffer(text)
		if amount == nil {
			whatsapp.SendText(from, "I could not read the amount in that. Try: set allowance 400.")
			return true
		}
		if err := catalogue.SetAllowance(merchant.ID, *amount); err != nil {
			whatsapp.SendText(from, "Nothing changed: "+err.Error()+".")
			return true
		}
		whatsapp.SendText(from, "Done. Your daily giveaway allowance is now "+money.GHS(*amount)+". Every discount I agree comes out of it, and I haggle firmer as it runs down.")
		return true
	}

	if m := removeRe.FindStringSubmatch(lower); m != nil {
		name := strings.TrimSpace(m[1])
		product := catalogue.FindByName(merchant.ID, name)
		if product == nil {
			whatsapp.SendText(from, "I cannot find \""+name+"\" in your catalogue. Say \"my catalogue\" to see what is there.")
			return true
		}
		s.PendingRemove = product.Name
		whatsapp.SendText(from, "Remove "+product.Name+" from your catalogue? Customers will no longer be offered it. Say yes to confirm, or no to keep it.")
		return true
	}

	return false
}
