// Package insights is merchant insights in chat (replacing the
// merchant web page): the dashboard is the chat. The
// agent recognises the merchant's own number and answers plain
// questions with day book facts, in day language. Grounded strictly in
// what the day book recorded: the agent only knows what flowed through
// it, and says nothing it cannot count.
package insights

import (
	"strings"

	"github.com/valormarc/hackathon-agent/internal/daybook"
	"github.com/valormarc/hackathon-agent/internal/money"
)

// DayReportText is the day report: what the merchant hears after a
// sale and at end of day.
func DayReportText(day daybook.State) string {
	left := day.Allowance - day.GivenAway
	if left < 0 {
		left = 0
	}
	parts := []string{
		money.JSNumber(float64(day.Sales)) + " sale(s) today, " + money.GHS(day.Revenue) + " in.",
		"Given away in haggling: " + money.GHS(day.GivenAway) + " of your " + money.GHS(day.Allowance) + " allowance, " + money.GHS(left) + " left.",
	}
	if day.WalkAways != 0 {
		parts = append(parts, money.JSNumber(float64(day.WalkAways))+" customer(s) walked away from a final price.")
	}
	return strings.Join(parts, " ")
}
