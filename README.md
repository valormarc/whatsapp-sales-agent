# Stall

A WhatsApp sales agent for small shops. My entry for the Meta Global AI Developer Hackathon. Startup track, theme: Build for Your World.

I build software for small businesses. The sellers I work with run their shops on WhatsApp and on memory: prices in their heads, stock counted by eye, orders scattered across chats. They are never going to learn a dashboard, and most of them should never have to create accounts or connect anything to start selling. So Stall meets them where they already are: inside the chat they already use, on one shared number, with no app to install and nothing new to learn.

## How buying works

Every shop on Stall has its own code and its own link on the one shared number. A customer who arrives by a link lands in that shop. A customer who arrives with no code is not turned away: she says what she is looking for, and Stall checks every shop for it, shows her what exists and what it costs, and takes her into the right shop when she picks one.

Inside the shop, the customer browses the real catalogue, sees the product's own photos, and haggles the way traders haggle. The agent moves between the list price and the last price the merchant set, and never crosses it. A merchant can also mark a product fixed price, and then it is quoted as fixed and never haggled. Each shop has a daily giveaway allowance, so the day's discounts come out of one pot the merchant controls, and concessions get smaller as the pot drains.

When a price is agreed, the agent settles how the goods will reach her: delivery, her own pickup, a meet-up, or somebody she sends to collect. It takes the name for the package and any message she wants the seller to see, then reads the whole order back to her with Yes and Change buttons before any money moves. Only her yes produces the payment link, a MoMo checkout through Seev. When she says she has paid, the agent checks with Seev itself and confirms to both sides; the merchant is never asked to verify a payment she cannot see. If a link dies before she pays, the agent mints a fresh one instead of leaving her stuck, and an unpaid order is checked against Seev again before anyone is reminded about it.

## How selling works

A merchant opens her shop in the same chat. Her own phone number is her key; she never creates a Facebook account, never touches a dashboard, and never connects a number of her own. Opening the shop also opens her own Seev subaccount, so money from her links settles to her own MoMo, not into one shared pot.

She lists goods the way she already shares them in her selling groups: she throws in a product photo with whatever words she has, or pastes an advert, or answers a few short questions. Muse Spark reads the photo or the advert and drafts the listing: the name, one short clean line of description, the price she stated, and whether the good is sold whole or by measure, a piece, a yard, a bottle. She is asked only for what she did not give, above all her last price, and nothing sells until the listing is saved with her numbers on it. Prices that look wrong, a figure read out of a spec sheet, say, are stopped and confirmed with her before the listing can proceed.

After that she runs the shop from the same chat: "my catalogue" shows her goods and prices, the agent tells her the moment a sale lands and exactly what she made after fees, and her questions about the day are answered from the day's own records.

## How it thinks

Stall is built on one rule: the model judges meaning, and code owns the facts.

The model reads what a person means, in whatever words they use, and drafts the replies in its own words each time. Code owns the catalogue, the prices, the last prices, the allowance and the payment state, and it checks every figure in a drafted reply against those facts before anything is sent. A price the model invents never reaches a customer. A buyer who refuses to pay for something she has not seen is understood as asking for a picture, whatever words she uses, and she gets the product's photo, or the agent asks the shop owner for it and passes it on. The same reading happens on the merchant's side: her questions are answered, her corrections are taken, and a step in her listing is never silently consumed by the next question.

Nothing here is a script of stored answers. The formats are shapes; the words are composed from the facts of each conversation, and then verified.

## An honest note on what is mine and when

The building period for this event runs from the kickoff on 26 October to 4 November 2026. I had explored this idea before the event: a foundation from 20 to 22 October, and a short spike of the model integration on the morning of 26 October, before the organisers confirmed that the building period opens after the kickoff session. That earlier work is mine too, but it is not what I am presenting. Everything in this repository is the event build, written this week.

I created this repository on the first day of the event. The build is published here as it is completed and tested.

## Stack

Go (standard library only), the Meta Model API (Muse Spark for conversation, negotiation, reading photos and adverts, and drafting), the WhatsApp Cloud API, and Seev for MoMo checkout.
