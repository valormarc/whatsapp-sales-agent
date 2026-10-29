# Stall

My entry for the Meta Global AI Developer Hackathon. Startup track, theme: Build for Your World.

I build software for small businesses. The sellers I work with run their shops on WhatsApp and on memory: prices in their heads, stock counted by eye, orders scattered across chats. They are never going to learn a dashboard. So Stall meets them where they already are.

A customer chats with the shop on WhatsApp in plain text. The agent answers from the shop catalogue, haggles the way a trader does, moving between the list price and a floor the merchant set and never crossing it, and closes the sale with a MoMo payment link. The merchant onboards by sending product photos on WhatsApp and answering a few questions in the same chat. No app to install, nothing new to learn.

The same chat also serves the merchant herself. She asks how today is going, what people keep asking for, where she is losing money, and gets straight answers from the day's records, plus a summary at close of day.

## What I am building this week

- WhatsApp channel on the Meta WhatsApp Cloud API
- Merchant onboarding in chat: she throws a product photo in with whatever words she has, the way sellers already post in their groups, and Muse Spark drafts the listing from the photo through the Meta Model API. Her price is read from her own words by code, never drafted, and she is asked only for what she did not give, her floor price above all. Nothing sells until she confirms the listing in the same chat
- A sales agent grounded on the merchant's own catalogue, which hands over to the merchant when it is unsure instead of guessing
- A haggling engine with a secret floor price per product, enforced in code, not just in the prompt
- MoMo checkout through Seev, with the signed webhook settling the order and telling the merchant what she made
- One payment account per shop: opening a shop opens the shop's own Seev subaccount in the same chat, so her link money settles to her own MoMo (she keeps 99 percent) instead of one shared pot
- Merchant insights in the same chat: sale notifications, day questions answered in plain words, end of day summary

## An honest note on what is mine and when

The building period for this event runs from the kickoff on 26 October to 4 November 2026. I had explored this idea before the event: a foundation from 20 to 22 October, and a short spike of the model integration on the morning of 26 October, before the organisers confirmed that the building period opens after the kickoff session. That earlier work is mine too, but it is not what I am presenting. Everything in this repository is the event build, written this week.

I created this repository on the first day of the event. The build is published here as it is completed and tested.

## Stack

Go, the Meta Model API (Muse Spark for conversation, negotiation and drafting listings from product photos), the WhatsApp Cloud API, and Seev for MoMo checkout.
