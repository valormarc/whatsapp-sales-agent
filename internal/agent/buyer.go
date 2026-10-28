// The buyer's turn inside a shop, once her shop is resolved:
// payment claims on a finished sale first, then the stage she
// is in (agreed, fulfilment, confirmation), then browsing and
// haggling.
package agent

import (
	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/money"
	"github.com/valormarc/hackathon-agent/internal/whatsapp"
)

func handleBuyerTurn(from, boundID, msg, lower string) {
	merchant := catalogue.GetMerchant(boundID)
	if merchant == nil {
		delete(bindings, from)
		whatsapp.SendText(from, askShopText)
		return
	}
	shopID := merchant.ID
	key := sessionKey(shopID, from)
	s, ok := sessions[key]
	if !ok {
		s = &Session{Stage: "browsing", MerchantID: shopID}
		sessions[key] = s
	}

	// A place she states anywhere in the chat is kept on her
	// session, so the fulfilment step never asks for it twice.
	if s.Location == "" {
		if loc := extractStatedLocation(msg); loc != "" {
			s.Location = loc
		}
	}

	// Paying for a sale already completed: choosing the direct
	// route to the merchant's own number, or claiming a direct
	// payment. Both attach to her own newest unpaid order in this
	// shop (payment.go), and run before the stage handling below:
	// a claim's words ("I have paid", "payment done") must never
	// be mistaken for a request to resend the link.
	if handleBuyerPayment(from, merchant, msg, lower) {
		return
	}

	// An agreed deal waiting on the buyer: resend the link on request.
	if s.Stage == "agreed" && linkRe.MatchString(lower) {
		whatsapp.SendText(from, "Here is your payment link: "+s.PaymentURL)
		return
	}

	// A price is agreed and the sale is waiting on how the goods
	// reach her: the fulfilment step owns her words until it
	// completes or she cancels.
	if s.Stage == "fulfilment" {
		handleFulfilment(from, s, merchant, msg, lower)
		return
	}

	// The confirmation step is open: the buyer said the warm words
	// ("I'll buy") and was quoted the price and asked plainly. Only
	// a clear yes here moves on, into the fulfilment step at the
	// quoted price; a no leaves the item on the shelf; anything
	// else is re-asked once, plainly, and then left open rather
	// than chased. A bare "ok" at THIS step is the clear yes,
	// because the question was explicit; everywhere else it is
	// only ever an acknowledgement. A restated figure is a yes
	// too: a buyer who answers by naming the quoted price ("I
	// will pay 165") has confirmed in her own words, and she is
	// not made to translate herself into a bare yes.
	if s.PendingClose {
		name := ""
		if s.Product != nil {
			name = s.Product.Name
		}
		switch {
		case isClearYes(lower):
			price := s.ClosePrice
			s.PendingClose = false
			beginFulfilment(from, s, price)
		case restatesQuotedPrice(msg, s, merchant):
			price := s.ClosePrice
			s.PendingClose = false
			beginFulfilment(from, s, price)
		case isClearNo(lower):
			clearPendingClose(s)
			whatsapp.SendText(from, "No problem. The "+name+" is here whenever you are ready.")
		case s.CloseReasked:
			clearPendingClose(s)
			whatsapp.SendText(from, "I will leave it there for now. The "+name+" is here whenever you are ready.")
		default:
			s.CloseReasked = true
			whatsapp.SendText(from, "Shall I send the payment link for the "+name+" at "+money.GHS(s.ClosePrice)+"? Say yes or no.")
		}
		return
	}

	// Checking an order she already made: she quotes the
	// reference she was given at the sale, or asks after her
	// order, and it is read back to her (ordercheck.go). The
	// check is read-only and leaves the stage she is in exactly
	// where it stood.
	if handleOrderCheck(from, merchant, msg, lower) {
		return
	}

	// Delivery, and the one total. Real buyers ask about delivery
	// before the item price as often as after it, and they want one
	// figure, not two to add up. Asked while a price is in play, the
	// answer carries that total; the haggle state is untouched.
	if (s.Stage == "browsing" || s.Stage == "haggling") && parseOffer(lower) == nil &&
		(totalRe.MatchString(lower) || deliveryRe.MatchString(lower)) {
		if answerDeliveryOrTotal(from, s, merchant, lower) {
			return
		}
	}

	// The final stand: one price is on the table. Meet it, say the
	// word, or part as friends. The agent does not chase a losing sale.
	if s.Stage == "lastprice" {
		// A bare acknowledgement is never the word: "Ok" here means
		// she heard the price, not that she took it. The stand is
		// held up again, and nothing closes.
		if isBareAck(lower) {
			whatsapp.SendText(from, "My last price stands at "+money.GHS(s.Ask)+" for the "+s.Product.Name+". Say yes and I will send the payment link, or feel free to look around.")
			return
		}
		if totalRe.MatchString(lower) || deliveryRe.MatchString(lower) {
			if answerDeliveryOrTotal(from, s, merchant, lower) {
				return
			}
		}
		offer := parseOffer(lower)
		if offer != nil {
			if *offer >= s.Ask {
				beginFulfilment(from, s, s.Ask)
			} else {
				p := s.Product
				lastPrice := s.Ask
				daybook.Default.RecordWalkAway(shopID, daybook.Options{Allowance: merchant.DailyGiveawayAllowance})
				sessions[key] = &Session{Stage: "browsing", MerchantID: shopID}
				// The let-go carries the next step: the total with
				// delivery, and the link ready the moment she says
				// so. A bare "that is final" is where real chats go
				// silent and die.
				if fee := deliveryFeeOf(merchant); fee > 0 {
					next := "That is " + money.GHS(lastPrice+fee) + " with delivery, and I can send the payment link the moment you say so."
					if deliveryPaidToOf(merchant) == "carrier" {
						next = "That is " + money.GHS(lastPrice) + " with me, plus " + money.GHS(fee) + " delivery paid to " + carrierTarget(merchant.DeliveryNote) + " when it reaches you. I can send the payment link the moment you say so."
					}
					sendPieces(from,
						"No problem at all. "+money.GHS(*offer)+" would leave me at a loss on this one, so I will let you go with thanks. The "+p.Name+" is here at "+money.GHS(lastPrice)+" whenever you are ready.",
						next)
				} else {
					whatsapp.SendText(from, "No problem at all. "+money.GHS(*offer)+" would leave me at a loss on this one, so I will let you go with thanks. The "+p.Name+" is here at "+money.GHS(lastPrice)+" whenever you are ready.")
				}
			}
			return
		}
		if yesRe.MatchString(lower) {
			beginFulfilment(from, s, s.Ask)
			return
		}
		whatsapp.SendText(from, "My last price stands at "+money.GHS(s.Ask)+" for the "+s.Product.Name+". Say yes and I will send the payment link, or feel free to look around.")
		return
	}

	// Mid-haggle: a bare number (or "offer N") is the buyer's next move.
	if s.Stage == "haggling" {
		// A quantity phrase mid-haggle belongs to the measure
		// behaviour first: a new quantity restarts the haggle on
		// the scaled figures for it, and a quantity is never read
		// as the offer itself (see measure.go).
		if handleQuantity(from, s, merchant, msg, lower) {
			return
		}
		offer := parseOffer(lower)
		if offer != nil && !howMuchRe.MatchString(lower) {
			processOffer(from, s, merchant, *offer, msg)
			return
		}
		// Commitment words with no price in them ("I'll buy") move
		// to the confirmation step at the standing price: the buyer
		// is quoted the one total and asked plainly. Her "I'll buy"
		// alone closes nothing.
		if buyIntentRe.MatchString(lower) {
			startConfirmation(from, s, merchant, s.Product, s.Ask, false)
			return
		}
		// She is talking, not bidding. In the real groups a vague
		// turn ("any type") never ends the talk: the seller holds
		// the standing price up and keeps the door open. Only a
		// turn naming a different item leaves this haggle.
		other := catalogue.FindProduct(shopID, lower)
		if other == nil || (s.Product != nil && other.ID == s.Product.ID) {
			p := s.Product
			if picRe.MatchString(lower) {
				whatsapp.SendText(from, "I cannot send a photo of the "+p.Name+" from here, but it is this: "+p.Description+" The price stands at "+money.GHS(s.Ask)+". Give me your price and let us talk.")
				return
			}
			// The holding reply rotates: the same line twice in one
			// chat reads like a machine, so the wording moves while
			// the figure and the meaning stay exactly the same.
			holds := []string{
				"The " + p.Name + " stands at " + money.GHS(s.Ask) + ". Give me your price and let us see.",
				"We are still on the " + p.Name + " at " + money.GHS(s.Ask) + ". What price works for you?",
				"The " + p.Name + " is at " + money.GHS(s.Ask) + " for now. Talk to me, give me your price.",
			}
			whatsapp.SendText(from, holds[s.Holds%len(holds)])
			s.Holds++
			return
		}
	}

	// A bare acknowledgement while browsing ("ok", "ayt", "noted"):
	// she heard us, nothing more. It settles nothing and invents no
	// sale; the door stays open with the item she was looking at, or
	// an invitation to name one. The wording rotates like the
	// holding reply's does.
	if s.Stage == "browsing" && isBareAck(lower) {
		var variants []string
		if s.LastViewed != nil {
			p := s.LastViewed
			variants = []string{
				"The " + p.Name + " is here at " + money.GHS(p.ListPrice) + " whenever you are ready.",
				"Still here: the " + p.Name + " at " + money.GHS(p.ListPrice) + ". Say the word when you want it.",
				"No rush at all. The " + p.Name + " stands at " + money.GHS(p.ListPrice) + ".",
			}
		} else {
			variants = []string{
				"No problem. Tell me what you are after and I will sort you out.",
				"Alright. What are you looking for today?",
				"Ok, I am here. Name the item and I will price it for you.",
			}
		}
		whatsapp.SendText(from, variants[s.Acks%len(variants)])
		s.Acks++
		return
	}

	// Quantities while browsing: a measured amount is quoted,
	// refused or clarified by the measure behaviour, and the
	// quantity she settles on is remembered for the offer that
	// follows (see measure.go).
	if s.Stage == "browsing" {
		if handleQuantity(from, s, merchant, msg, lower) {
			return
		}
	}

	// The lane. People put general questions to any AI they can
	// reach: what a word means, a sum, who the president is. This
	// agent works for the merchant and answers only about her
	// shop, so a clear off-topic turn is steered back (laneStrike)
	// and never answered itself. The check runs only when the
	// message carries no shop signal at all: anything naming a
	// product, a price, a want, a delivery or an availability ask
	// is the shop's business and is handled by the branches below,
	// never steered. A bare figure is not a shop signal here on
	// purpose: "what is 5 + 5" carries figures, but it is a sum,
	// not an offer, and the arithmetic pattern settles it.
	{
		_, namedAsk := unstockedAsk(lower)
		shopish := catalogue.FindProduct(shopID, lower) != nil ||
			intentRe.MatchString(lower) || howMuchRe.MatchString(lower) ||
			wantRe.MatchString(lower) || needRe.MatchString(lower) ||
			greetingRe.MatchString(lower) || buyIntentRe.MatchString(lower) ||
			picRe.MatchString(lower) || deliveryRe.MatchString(lower) ||
			totalRe.MatchString(lower) || namedAsk
		switch {
		case s.LaneClosed && !shopish && (parseOffer(lower) == nil || arithmeticRe.MatchString(lower) || offTopicRe.MatchString(lower)):
			// The lane is closed on this buyer: her off-topic
			// turns cost the merchant nothing, not even a reply.
			// A plain figure still falls through below: a number
			// after all that silence is a buyer trying to buy.
			return
		case !shopish && (arithmeticRe.MatchString(lower) || offTopicRe.MatchString(lower)):
			laneStrike(from, s, merchant)
			return
		case shopish || parseOffer(lower) != nil:
			// Talk about the shop reopens the lane and clears
			// the strikes, the way a returning customer is just
			// a customer again.
			s.OffTopic = 0
			s.LaneClosed = false
		}
	}

	// A buyer who jumps straight to an offer, the way people actually
	// talk: a price named in the same breath as an item, or a price
	// right after looking at one item, is an offer for that item. She
	// should never need the perfect question to be understood. A
	// price with no item in sight gets the human question back:
	// which item is it for.
	if s.Stage == "browsing" {
		if offer := parseOffer(lower); offer != nil {
			target := catalogue.FindProduct(shopID, lower)
			if target == nil {
				target = s.LastViewed
			}
			if target != nil {
				loc := s.Location
				// An offer right after a measured quote lands on
				// the quantity she was quoted, for that same
				// item: the haggle opens on the scaled figures.
				if s.Qty > 0 && s.LastViewed != nil && target.ID == s.LastViewed.ID && target.ByMeasure() {
					s = openHaggleQty(shopID, merchant, target, s.Qty)
				} else {
					s = openHaggle(shopID, merchant, target)
				}
				s.Location = loc // a fresh haggle never forgets where she is
				sessions[key] = s
				processOffer(from, s, merchant, *offer, msg)
				return
			}
			whatsapp.SendText(from, "I want to get you the right price. Which item is that offer for?\n"+productListText(shopID))
			return
		}
	}

	// Commitment words while browsing ("I'll buy", "I'll take it"),
	// naming an item or following one she just viewed: quote the one
	// total and ask plainly. The words themselves close nothing;
	// only her yes at the confirmation step does, at the list price.
	if s.Stage == "browsing" && buyIntentRe.MatchString(lower) {
		target := catalogue.FindProduct(shopID, lower)
		if target == nil {
			target = s.LastViewed
		}
		if target != nil {
			startConfirmation(from, s, merchant, target, target.ListPrice, true)
			return
		}
	}

	// "Send pic": in the real groups this is a buyer's first move
	// once an item has her eye. There is no photo to send from here,
	// so the agent says so plainly and puts the item and its price
	// in front of her instead of going blank.
	if picRe.MatchString(lower) {
		target := catalogue.FindProduct(shopID, lower)
		if target == nil {
			target = s.LastViewed
		}
		if target != nil {
			s.LastViewed = target
			whatsapp.SendText(from, "I cannot send a photo of the "+target.Name+" from here, but it is this: "+target.Description+" It goes for "+money.GHS(target.ListPrice)+". Make me an offer and let us see.")
			return
		}
	}

	// Price question or product mention: quote and open the haggle,
	// with the day book setting the posture for this customer.
	product := catalogue.FindProduct(shopID, lower)
	if product != nil && intentRe.MatchString(lower) {
		loc := s.Location
		s = openHaggle(shopID, merchant, product)
		s.Location = loc // a fresh haggle never forgets where she is
		sessions[key] = s
		whatsapp.SendText(from, product.Name+": "+product.Description+" The price is "+money.GHS(product.ListPrice)+". Make me an offer and let us see.")
		return
	}
	if product != nil {
		// A completed sale leaves her session on the agreed
		// stage. Looking at goods again means she is shopping
		// again, so the session goes back to browsing: a bare
		// offer on what she is looking at must bind on her second
		// purchase exactly as it did on her first.
		if s.Stage == "agreed" {
			s.Stage = "browsing"
		}
		// Looking at a different item ends the old item's pending
		// quantity: a remembered measure belongs to what it was
		// quoted on, never to whatever she looks at next.
		if s.LastViewed == nil || s.LastViewed.ID != product.ID {
			s.Qty, s.Unit = 0, ""
		}
		s.LastViewed = product
		view := product.Name + ": " + product.Description + " It goes for " + money.GHS(product.ListPrice) + ". Want to make an offer?"
		if availabilityFramed(lower) {
			// She asked in the availability frame, so the
			// answer leads with the market's own plain words
			// for it, then the goods.
			sendPieces(from, "Still available.", view)
		} else {
			whatsapp.SendText(from, view)
		}
		return
	}

	// The need-led opening: the buyer describes a problem, not a
	// product ("something to keep the moisture", "you understand
	// what I mean right?"), and a real seller understands her,
	// recommends the one thing on her own shelf that answers it,
	// and anchors the price early. The recommendation is grounded
	// in this shop's catalogue only, by word overlap with the
	// goods' names and descriptions. Nothing matching means the
	// shelf is offered and the want is written down, as below.
	if needRe.MatchString(lower) {
		if rec := recommendForNeed(shopID, lower); rec != nil {
			s.LastViewed = rec
			sendPieces(from,
				"I understand what you mean.",
				"For that, the "+rec.Name+" is the one: "+rec.Description+" It goes for "+money.GHS(rec.ListPrice)+".")
			return
		}
		recordWantAndOfferShelf(from, s, msg)
		return
	}

	// The bare want naming nothing we sell: "I need a tv" in a shop
	// with no televisions. A real seller does not go blank at it;
	// she says what she does have and asks which one the buyer is
	// after. And she writes the want down: if it ever lands on her
	// shelf, this buyer hears about it (see recordWantAndOfferShelf).
	if wantRe.MatchString(lower) {
		recordWantAndOfferShelf(from, s, msg)
		return
	}

	if greetingRe.MatchString(lower) {
		sendPieces(from, "Welcome to "+merchant.Name+".", productListText(shopID))
		return
	}

	// The availability question naming something this shop does
	// not stock: "do you have a rice cooker?" It carries no want
	// word, so it used to fall past the want path into the generic
	// fallback and the want was never written down. It reaches
	// here only after the shelf has had its say (a stocked item is
	// answered as an item further up), so what it names is a want
	// like any other, and it is recorded the same way.
	if item, ok := unstockedAsk(lower); ok {
		if p := stockedByLooseName(shopID, item); p != nil {
			// She named something on the shelf in words the
			// stricter product matching missed ("bags" for the
			// beaded bag): answer the item itself, and never
			// write a want for goods already in the shop.
			s.LastViewed = p
			sendPieces(from, "Still available.", p.Name+": "+p.Description+" It goes for "+money.GHS(p.ListPrice)+". Want to make an offer?")
			return
		}
		recordWantAndOfferShelf(from, s, msg)
		return
	}

	whatsapp.SendText(from, "Tell me what you are after and I will sort you out.\n"+productListText(shopID))
}
