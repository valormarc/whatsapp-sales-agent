// The front door: what a stranger with no shop meets on a
// bare greeting, one message with Buy and Sell buttons.
package agent

import (
	"regexp"
)

// doorText is the front door a stranger meets on a bare greeting:
// one short message carrying two reply buttons, Buy and Sell, so
// her first step is a tap instead of homework. It lives only here,
// at the cold door: a merchant is known by her number and a buyer
// by her shop link, and neither of them is ever shown it. Typing
// still works exactly as it always did; the buttons are a
// shortcut, never a gate.
const doorText = "Hello, and welcome. I sell for many shops on this one number. If you came to buy, tap Buy and send me the shop code the seller shared with you. If you want to sell, tap Sell and we will open your shop."

// bareGreetingRe matches a message that is only a greeting, the
// knock the door answers: hi, hello, good morning and their kin,
// with nothing else in it. (greetingRe above is the looser
// starts-with-a-greeting matcher used inside a shop.)
var bareGreetingRe = regexp.MustCompile(`^(hi+|hello|hey+|good (morning|afternoon|evening)|akwaaba)[\s!.]*$`)
