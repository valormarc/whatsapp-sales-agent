# Setting up the details forms

The agent collects two sets of details through WhatsApp Flows, the
platform's own slide-up form panels:

- **delivery-details.json** — a buyer's delivery details: the name for
  the package, the number the rider can call, the town, and a landmark.
  Offered beside the package-name question whenever goods are being
  sent, and answered in one screen instead of a walk.
- **payout-details.json** — a merchant's payout details: the name on
  her MoMo account and the number itself. Opened when she says
  "payout details" in her chat.

The chat path always works without them; a form is a shortcut for
fingers, never a gate. Until the forms exist on the WhatsApp account
and their ids are configured, the offers simply never appear.

## Creating the forms (once, on the WhatsApp account)

The account's API token for messaging does not carry the
business-management permission Flows need, so the forms are created
in WhatsApp Manager:

1. Open business.facebook.com and choose the Stall Commerce WhatsApp
   account.
2. Go to WhatsApp Manager, then Account tools, then Flows.
3. Create a flow. Name the first one "Stall Delivery Details". When
   asked for a JSON definition, paste the whole of
   delivery-details.json. Publish it, and copy its Flow ID.
4. Do the same for "Stall Payout Details" with payout-details.json.
5. Put the ids in the agent's environment and restart it:

   STALL_FLOW_DELIVERY_ID=<the delivery flow's id>
   STALL_FLOW_PAYOUT_ID=<the payout flow's id>

That is the whole setup. The agent reads the ids at work; no code
changes when the forms change, only the JSON above and a re-upload.
