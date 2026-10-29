// Live proof for the scam protections, with the real model
// key. A one-off item's sale waits at the in-hand gate (no
// payment button, no order) until the seller confirms in
// her own chat; a buyer about to pay a new shop is told the
// shop is new; and a buyer who calls a paid sale a scam —
// in her own words, read by the model — gets a dispute
// opened and frozen on Stall's side with the rail's limit
// stated plainly, the seller told at once, and a claimed
// refund that counts only when the buyer confirms it.
const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

if (!process.env.META_MODEL_API_KEY) { console.log("META_MODEL_API_KEY required"); process.exit(1); }

const PORT = 3495;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "trust-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER1 = "233240000991", OWNER2 = "233240000992";
const B1 = "233551119991", B2 = "233551119992";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    GATE: { id: "GATE", shopCode: "GATE", name: "Gate Cloths", ownerWhatsApp: OWNER1,
      payoutPhone: OWNER1, dailyGiveawayAllowance: 300,
      products: [{ id: "kente", name: "Kente Cloth", description: "Handwoven kente", listPrice: 400, floorPrice: 350, stock: 1 }] },
    TRUST: { id: "TRUST", shopCode: "TRUST", name: "Trust Bags", ownerWhatsApp: OWNER2,
      payoutPhone: OWNER2, dailyGiveawayAllowance: 300,
      products: [{ id: "bag", name: "Handmade Beaded Bag", description: "Hand-beaded evening bag", listPrice: 850, floorPrice: 680, stock: 3 }] },
  },
}));
// A paid order in the book for the dispute scenario: the
// buyer ordered and paid before this conversation.
fs.writeFileSync(path.join(dir, "data", "orders.json"), JSON.stringify({
  "TRUST-0001": { reference: "TRUST-0001", from: B2, createdAt: new Date().toISOString(),
    productName: "Handmade Beaded Bag", price: 850, total: 850, status: "paid",
    merchantId: "TRUST", paymentPath: "seev", fulfilment: "delivery", location: "Accra" },
}));
const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1" };
delete env.SEEV_SECRET_KEY; delete env.SEEV_API_KEY;
const agent = spawn(path.join(__dirname, "..", "agent"), [], { env, stdio: "ignore" });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let seq = 0;
async function msg(from, text) {
  seq++;
  await fetch(`${BASE}/webhook`, { method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ object: "whatsapp_business_account", entry: [{ changes: [{ value: { messages: [{ from, id: "w" + seq, type: "text", text: { body: text } }] } }] }] }) });
}
function outboxTo(who) {
  const f = path.join(dir, "data", "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)).filter((r) => r.to === who);
}
async function say(from, text) {
  const before = outboxTo(from).length;
  await msg(from, text);
  let lastCount = -1, stable = 0;
  for (let i = 0; i < 50; i++) {
    await sleep(600);
    const n = outboxTo(from).length;
    if (n === lastCount) { stable++; } else { stable = 0; lastCount = n; }
    if (n > before && stable >= 4) break;
    if (i > 40 && n === before && stable >= 4) break;
  }
  await sleep(400);
  return outboxTo(from).slice(before);
}
const bodies = (rs) => rs.map((r) => r.body || "").join("\n");
function orders() {
  const f = path.join(dir, "data", "orders.json");
  if (!fs.existsSync(f)) return [];
  return Object.values(JSON.parse(fs.readFileSync(f, "utf8")));
}

(async () => {
  let pass = 0, fail = 0;
  const check = (name, ok) => { console.log((ok ? "PASS " : "FAIL ") + name); ok ? pass++ : fail++; };
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }

    // A. The one-off gate.
    await say(B1, "SHOP-GATE");
    await say(B1, "Kente Cloth");
    await say(B1, "400");
    await say(B1, "I will pick it up myself");
    await say(B1, "Akosua Mensah");
    await say(B1, "no");
    const gated = await say(B1, "yes");
    check("the one-off sale waits: the buyer is told why", bodies(gated).includes("only one the shop has"));
    check("no payment button before the seller's word", !outboxTo(B1).some((r) => r.kind === "cta_url"));
    check("the seller is asked in her own chat", bodies(outboxTo(OWNER1)).includes("Do you have it in hand now?"));
    check("no GATE order exists yet", !orders().some((o) => o.reference.startsWith("GATE")));
    await say(OWNER1, "yes");
    check("her yes sends the payment button", outboxTo(B1).some((r) => r.kind === "cta_url"));
    check("the order is recorded after her yes", orders().some((o) => o.reference.startsWith("GATE")));
    check("the buyer of a new shop was told the shop is new", bodies(outboxTo(B1)).includes("is new on Stall"));

    // B. The dispute, in the buyer's own angry words.
    await say(B2, "SHOP-TRUST");
    const opened = await say(B2, "This woman is a scammer. She took my money for the bag and sent nothing, I want my money back");
    check("the dispute opens on her meaning", bodies(opened).includes("opened a dispute on order TRUST-0001"));
    check("the freeze is stated honestly, rail limit included", bodies(opened).includes("cannot pull back money"));
    check("the seller is told at once", bodies(outboxTo(OWNER2)).includes("Dispute opened"));
    check("the order stands frozen open", (orders().find((o) => o.reference === "TRUST-0001") || {}).disputeState === "open");
    await say(OWNER2, "I have refunded order TRUST-0001");
    check("a claimed refund is put to the buyer", bodies(outboxTo(B2)).includes("Have you received the refund?"));
    await say(B2, "yes");
    check("her yes closes the dispute clean", (orders().find((o) => o.reference === "TRUST-0001") || {}).disputeState === "resolved_refund");
    const tf = path.join(dir, "data", "trust.json");
    const strikes = fs.existsSync(tf) ? Object.values(JSON.parse(fs.readFileSync(tf, "utf8"))).reduce((n, r) => n + (r.strikes || 0), 0) : 0;
    check("no strike stands after a confirmed refund", strikes === 0);
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    agent.kill("SIGKILL");
    console.log(`\ntrust live proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
