package agent

// Settlement words are records now (see records.go): when a
// payment is confirmed, the buyer hears the payment
// confirmed record and the seller hears the order notice,
// settled, both printed from the order's own facts on the
// fixed paper, the same on every road that settles. The
// composers registered here are the two hooks the
// settlement path calls, so the webhook, the agent's own
// read-back and the unpaid watch all print the same paper.

import (
	"strings"
	"time"

	"github.com/valormarc/hackathon-agent/internal/catalogue"
	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/model"
	"github.com/valormarc/hackathon-agent/internal/orders"
	"github.com/valormarc/hackathon-agent/internal/seevwebhook"
)

func init() {
	seevwebhook.BuyerText = ComposeBuyerSettleText
	seevwebhook.MerchantNoticeText = ComposeMerchantSettleText
}

// ComposeBuyerSettleText is the registered composer behind
// seevwebhook.BuyerText: the buyer's payment confirmed
// record, printed from her order.
func ComposeBuyerSettleText(o *orders.Order, _ string) string {
	if o == nil {
		return ""
	}
	return paymentConfirmedRecord(o)
}

// ComposeDirectSettleText is the same record for the
// direct path, where the confirmation is the seller's own
// word on money she received on her number: the facts the
// record prints are the order's, and they are the same.
func ComposeDirectSettleText(o *orders.Order) string {
	if o == nil {
		return ""
	}
	return paymentConfirmedRecord(o)
}

// ComposeMerchantSettleText is the registered composer
// behind seevwebhook.MerchantNoticeText: the seller's
// order notice at settlement, with the day's figures read
// at the moment it prints.
func ComposeMerchantSettleText(o *orders.Order, fee, net float64) string {
	if o == nil {
		return ""
	}
	merchant := catalogue.GetMerchant(o.MerchantID)
	allowance := 0.0
	if merchant != nil {
		allowance = merchant.DailyGiveawayAllowance
	}
	day := daybook.Default.GetDayState(o.MerchantID, daybook.Options{Allowance: allowance})
	return settlementNoticeRecord(o, merchant, fee, net, day)
}

// draftWithBudget runs one drafting call with a patience
// budget. Composed words are worth a few seconds of a
// buyer's wait, never more: past the budget the caller falls
// back to its bare factual net, and the late draft is
// discarded. A reply must never be held hostage by a slow
// model.
func draftWithBudget(system, facts string) string {
	ch := make(chan string, 1)
	go func() {
		r := model.Chat([]model.Message{{Role: "user", Content: facts}}, model.Options{System: &system, Kind: model.KindDraft})
		if r == nil {
			ch <- ""
		} else {
			ch <- strings.TrimSpace(*r)
		}
	}()
	select {
	case d := <-ch:
		return d
	case <-time.After(6 * time.Second):
		return ""
	}
}
