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

// dayMoney prepares the day's money figures for any answer
// composed from them: rounded to whole cedis, with the left
// taken from the rounded figures. The book keeps what the
// day's arithmetic produced, and a subtraction there can
// carry a raw float's tail (25.999999999999986); a figure
// prepared as a fact for an answer never does, so no answer
// composed or drafted from these facts can carry one either.
func dayMoney(day daybook.State) (revenue, given, allowance, left float64) {
	revenue = money.Round(day.Revenue)
	given = money.Round(day.GivenAway)
	allowance = money.Round(day.Allowance)
	left = allowance - given
	if left < 0 {
		left = 0
	}
	return
}

// DayReportLines is the day report as separate short lines, for
// messages laid out line by line: the same figures as
// DayReportText, with the counts worded the way a person says
// them (one sale, two sales).
func DayReportLines(day daybook.State) []string {
	revenue, given, allowance, left := dayMoney(day)
	sales := "sales"
	if day.Sales == 1 {
		sales = "sale"
	}
	lines := []string{
		"Today: " + money.JSNumber(float64(day.Sales)) + " " + sales + ", " + money.GHS(revenue) + " in.",
		"Given away: " + money.GHS(given) + " of your " + money.GHS(allowance) + " allowance, " + money.GHS(left) + " left.",
	}
	if day.WalkAways != 0 {
		who := "customers"
		if day.WalkAways == 1 {
			who = "customer"
		}
		lines = append(lines, money.JSNumber(float64(day.WalkAways))+" "+who+" walked away from a final price.")
	}
	return lines
}

// DayReportText is the day report: what the merchant hears after a
// sale and at end of day.
func DayReportText(day daybook.State) string {
	revenue, given, allowance, left := dayMoney(day)
	parts := []string{
		money.JSNumber(float64(day.Sales)) + " sale(s) today, " + money.GHS(revenue) + " in.",
		"Given away in haggling: " + money.GHS(given) + " of your " + money.GHS(allowance) + " allowance, " + money.GHS(left) + " left.",
	}
	if day.WalkAways != 0 {
		parts = append(parts, money.JSNumber(float64(day.WalkAways))+" customer(s) walked away from a final price.")
	}
	return strings.Join(parts, " ")
}
