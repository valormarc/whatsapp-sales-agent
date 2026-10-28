// The front door: what a stranger with no shop meets on a
// bare greeting, one message with Buy and Sell buttons.
package agent

import (
	"regexp"
	"time"
)

// doorTextAt is the front door a stranger meets on a bare
// greeting: one short message carrying two reply buttons, Buy and
// Sell, so her first step is a tap instead of homework. It lives
// only here, at the cold door: a merchant is known by her number
// and a buyer by her shop link, and neither of them is ever shown
// it. Typing still works exactly as it always did; the buttons are
// a shortcut, never a gate. The greeting it opens with follows the
// time of day in Ghana, the way a person at a stall would say it.
func doorTextAt(hour int) string {
	greeting := "Hello"
	switch {
	case hour >= 5 && hour < 12:
		greeting = "Good morning"
	case hour >= 12 && hour < 17:
		greeting = "Good afternoon"
	case hour >= 17 && hour < 21:
		greeting = "Good evening"
	}
	return greeting + ", and welcome. I sell for many shops on this one number. If you came to buy, tap Buy and send me the shop code the seller shared with you. If you want to sell, tap Sell and we will open your shop."
}

// doorText is the door worded for this moment in Ghana.
func doorText() string {
	accra, err := time.LoadLocation("Africa/Accra")
	if err != nil {
		return doorTextAt(12)
	}
	return doorTextAt(time.Now().In(accra).Hour())
}

// bareGreetingRe matches a message that is only a greeting, the
// knock the door answers: the plain hellos, the time-of-day
// greetings people actually open with (good morning through good
// night, good day included), the two said together ("hello good
// day"), and the Twi day greetings maakye, maaha and maadwo, with
// nothing else in the message. (greetingRe above is the looser
// starts-with-a-greeting matcher used inside a shop.)
var bareGreetingRe = regexp.MustCompile(`^(?:(?:hi+|hello|hey+)(?:[\s,]+good\s+(?:morning|afternoon|evening|day|night))?|good\s+(?:morning|afternoon|evening|day|night)|akwaaba|maakye|maaha|maadwo)[\s!.]*$`)
