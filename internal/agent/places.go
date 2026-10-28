// Reading places and people out of a buyer's words: where
// goods go, where a meet-up happens, and who is coming to
// collect, each read conservatively.
package agent

import (
	"regexp"
	"strings"
)

// placeWordRe is a single word of a place name: letters, with the
// hyphens and apostrophes real place names carry.
var placeWordRe = regexp.MustCompile(`^[A-Za-z][A-Za-z'\-]*$`)

// placeExtraFiller are words that make a short message a sentence
// rather than a place, beyond askFillerWords (which already holds
// the pronouns, question words and shop talk).
var placeExtraFiller = map[string]bool{
	"i": true, "am": true, "will": true, "would": true, "shall": true,
	"there": true, "here": true, "just": true, "now": true, "then": true,
	"so": true, "well": true, "today": true, "tomorrow": true,
}

// cleanPlace reads a place out of a short message, conservatively:
// the text up to the first punctuation, a leading preposition or
// "the" dropped, one to four words, every word a plain word and
// none of them filler. Anything else reads as no place at all, and
// the caller asks or moves on rather than guessing.
func cleanPlace(raw string) string {
	t := strings.TrimSpace(raw)
	if i := strings.IndexAny(t, ",.;!?"); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	low := strings.ToLower(t)
	for _, pre := range []string{"to ", "in ", "at ", "the "} {
		if strings.HasPrefix(low, pre) {
			t = strings.TrimSpace(t[len(pre):])
			break
		}
	}
	words := strings.Fields(t)
	if len(words) == 0 || len(words) > 4 {
		return ""
	}
	for _, w := range words {
		if !placeWordRe.MatchString(w) {
			return ""
		}
		lw := strings.ToLower(w)
		if askFillerWords[lw] || placeExtraFiller[lw] {
			return ""
		}
	}
	return strings.Join(words, " ")
}

// barePlace reads a message that is nothing but a place: at the
// choice question, a buyer who answers "where are you" with a bare
// capitalised name has answered delivery, to that place. The
// capital is the signal; a lower-case fragment is conversation,
// not a place, and never completes a sale on a guess.
func barePlace(msg string) string {
	loc := cleanPlace(msg)
	if loc == "" {
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(msg))
	if len(fields) == 0 || len(fields) > 3 {
		return ""
	}
	if r := []rune(fields[0]); r[0] < 'A' || r[0] > 'Z' {
		return ""
	}
	return loc
}

var statedLocationRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:i am|i'm|im)\s+in\s+([A-Za-z][A-Za-z \-]{1,30})`),
	regexp.MustCompile(`(?i)\bi\s+dey\s+([A-Za-z][A-Za-z \-]{1,30})`),
	regexp.MustCompile(`(?i)\b(?:based|located)\s+in\s+([A-Za-z][A-Za-z \-]{1,30})`),
	regexp.MustCompile(`(?i)\bi\s+live\s+in\s+([A-Za-z][A-Za-z \-]{1,30})`),
	regexp.MustCompile(`(?i)\bmy\s+location\s+is\s+([A-Za-z][A-Za-z \-]{1,30})`),
}

// extractStatedLocation reads the place from a plain statement of
// where the buyer is ("I am in Koforidua", "I dey Accra", "based
// in Osu"). It is used wherever she says it, browsing, haggling or
// at the fulfilment questions, and the session keeps it.
func extractStatedLocation(msg string) string {
	for _, re := range statedLocationRes {
		if m := re.FindStringSubmatch(msg); m != nil {
			if loc := cleanPlace(m[1]); loc != "" {
				return loc
			}
		}
	}
	return ""
}

var deliveryToRe = regexp.MustCompile(`(?i)\bto\s+([A-Za-z][A-Za-z \-]{1,30})`)

// extractDeliveryPlace reads where delivery should go: a stated
// location, or the place after "to" in a sending message ("send
// it to Koforidua"). "Send it to your station" reads as no place:
// your is filler, so the question is asked instead.
func extractDeliveryPlace(msg string) string {
	if loc := extractStatedLocation(msg); loc != "" {
		return loc
	}
	if m := deliveryToRe.FindStringSubmatch(msg); m != nil {
		return cleanPlace(m[1])
	}
	return ""
}

var meetPlaceRe = regexp.MustCompile(`(?i)\bmeet\b[^,.]*?\bat\s+([A-Za-z][A-Za-z \-]{1,30})`)

// extractMeetPlace reads the meeting place out of a meet proposal
// ("let's meet at Circle", "meet me at the station"). A leading
// "the" is part of how the place is said, so it is kept.
func extractMeetPlace(msg string) string {
	m := meetPlaceRe.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	place := cleanPlace(m[1])
	if place == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(m[1])), "the ") {
		return "the " + place
	}
	return place
}

var (
	sendPersonRe       = regexp.MustCompile(`(?i)\bsend\s+((?:my|a|the)\s+[A-Za-z]+(?:\s+[A-Z][a-z]+)?|somebody|someone)`)
	personWillComeRe   = regexp.MustCompile(`(?i)\b(my\s+(?:brother|sister|rider|friend|cousin|mother|father|driver|boy|husband|wife)(?:\s+[A-Z][a-z]+)?)\s+will\s+come`)
	somebodyWillComeRe = regexp.MustCompile(`(?i)\b(somebody|someone)\s+will\s+come`)
)

// sentSomebody reads a collector arrangement: the buyer sends
// another person for the goods. It reports the message was such an
// arrangement at all, and whether someone is actually named (a
// person word or a description, "my brother Kwame", "my rider");
// a bare "somebody" or "someone" is an arrangement with nobody
// named yet, and the follow-up asks who.
func sentSomebody(msg string) (who string, named bool, ok bool) {
	if somebodyWillComeRe.MatchString(msg) {
		return "", false, true
	}
	if m := personWillComeRe.FindStringSubmatch(msg); m != nil {
		return strings.TrimSpace(m[1]), true, true
	}
	if m := sendPersonRe.FindStringSubmatch(msg); m != nil {
		who = strings.TrimSpace(m[1])
		if lw := strings.ToLower(who); lw == "somebody" || lw == "someone" {
			return "", false, true
		}
		return who, true, true
	}
	return "", false, false
}

// cleanCollector reads who is coming out of a short answer to
// "Who is coming for it?": her words as given, up to six plain
// words ("my brother Kwame", "Kwame"). Anything shaped otherwise
// is no answer, and the question is asked again.
func cleanCollector(msg string) string {
	t := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(msg), ".!?"))
	words := strings.Fields(t)
	if len(words) == 0 || len(words) > 6 {
		return ""
	}
	for _, w := range words {
		if !placeWordRe.MatchString(w) {
			return ""
		}
	}
	return strings.Join(words, " ")
}
