package server

import "net/http"

// The public pages the service is required to show: a privacy policy
// and terms of service. They are served by the agent itself so the
// addresses stay stable wherever the agent runs.

const pageStyle = `<style>
body { font-family: system-ui, sans-serif; margin: 2rem auto; max-width: 42rem; padding: 0 1rem; line-height: 1.55; color: #1a1a1a; }
h1 { font-size: 1.5rem; }
h2 { font-size: 1.1rem; margin-top: 1.8rem; }
</style>`

const privacyPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Privacy Policy</title>
` + pageStyle + `
</head>
<body>
<h1>Privacy Policy</h1>
<p>Effective 28 October 2026.</p>
<p>We run a WhatsApp sales assistant for small shops. A shop owner talks
to the assistant in WhatsApp to list what they sell, and their customers
talk to the same assistant to ask about goods, agree a price, arrange
how the goods will reach them, and pay. This page explains what
information passes through the service and what we do with it.</p>

<h2>What we collect</h2>
<p>Phone numbers of shop owners and of customers who message the
assistant. The messages themselves, including photographs of goods that
a shop owner sends to be listed and photographs a customer sends as
proof of a direct payment. The shop's catalogue: product names,
descriptions, prices and the lowest price the owner will accept. Order
details: the item, the agreed price, how the goods will be collected or
delivered, the place or person named for collection, and the payment
reference for the order.</p>

<h2>Why we collect it</h2>
<p>Only to run the shop's selling. The catalogue and prices are what
the assistant answers and negotiates from. Order details are what the
shop owner needs to hand over the goods and confirm payment. We do not
sell personal information, and we do not use it for advertising.</p>

<h2>Who else handles it</h2>
<p>WhatsApp messages travel through Meta's WhatsApp platform. Drafting
replies and product listings uses the Meta Model API. Card and mobile
money checkout is handled by Seev, our payment provider; we receive a
payment reference and a paid or unpaid status, not full payment
credentials. Each of those providers handles the information under its
own privacy terms.</p>

<h2>How long we keep it</h2>
<p>We keep a shop's catalogue and order records while the shop uses
the service, so the assistant can answer questions about past orders
and the owner can see their sales. Information that is no longer
needed is removed.</p>

<h2 id="deletion">Deleting your information</h2>
<p>Send the words "delete my data" to the assistant in WhatsApp, or
email us at v74y@outlook.com, and we will delete the personal
information we hold about you, including your messages and order
details, within 30 days. A shop owner can also remove any product from
their catalogue at any time by telling the assistant.</p>

<h2>Contact</h2>
<p>Questions about this policy: v74y@outlook.com.</p>
</body>
</html>`

const termsPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Terms of Service</title>
` + pageStyle + `
</head>
<body>
<h1>Terms of Service</h1>
<p>Effective 28 October 2026.</p>
<p>We provide a WhatsApp sales assistant that small shops use to sell
their goods. The assistant answers customers, negotiates within the
price bounds the shop owner sets, arranges collection or delivery as
the owner directs, and takes payment through our payment provider on
the owner's behalf.</p>

<h2>The shop's responsibilities</h2>
<p>The shop owner is responsible for their goods, their prices, the
accuracy of their listings and the delivery promises they make. The
assistant acts on the owner's instructions and never sells below the
lowest price the owner has set.</p>

<h2>Buying</h2>
<p>A customer's agreement is with the shop, not with us. Payment for
an order is collected through Seev or by mobile money to the shop's
own number, as the shop has chosen. An order is confirmed when payment
is confirmed.</p>

<h2>The service</h2>
<p>We provide the service as it stands and work to keep it running,
but we do not promise it will never be interrupted. We may change or
withdraw the service, and we will give shops notice in WhatsApp before
any change that affects their selling.</p>

<h2>Contact</h2>
<p>Questions about these terms: v74y@outlook.com.</p>
</body>
</html>`

func pageHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}
}
