// The shop desk proof, live with the real model key: the
// three dashboard pieces the merchant asked for, exercised
// the way she will use them. Stock first: a product with
// one in stock gates its sale for her in-hand word, and
// after she sets the count to five the same sale runs
// straight through. Orders: her list shows the sale the
// moment it lands. Cancel: she cancels an order, the buyer
// is told at once, the order reads cancelled in her list,
// and the day book gives the sale back. A paid order she
// cancels names the refund owed, and the buyer's dispute
// still opens on it afterwards.
const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

if (!process.env.META_MODEL_API_KEY) { console.log("META_MODEL_API_KEY required"); process.exit(1); }

const PORT = 3501;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "shopdesk-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });

const M = "233551300001";
const B1 = "233551300101", B2 = "233551300102", B9 = "233551300109";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    DESK: { id: "DESK", shopCode: "DESK", name: "Desk Shoes", ownerWhatsApp: M, payoutPhone: M,
      dailyGiveawayAllowance: 500,
      products: [{ id: "heels", name: "Ankle Strap Heels", description: "Pointed ankle-strap heels in mesh", listPrice: 120, floorPrice: 100, stock: 1 }] },
  },
}));
fs.writeFileSync(path.join(dir, "data", "orders.json"), JSON.stringify({
  "DESK-0007": { reference: "DESK-0007", from: B9, createdAt: new Date().toISOString(),
    productName: "Ankle Strap Heels", price: 120, total: 120, status: "paid",
    merchantId: "DESK", paymentPath: "seev", fulfilment: "pickup" },
}));

const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1" };
delete env.SEEV_SECRET_KEY; delete env.SEEV_API_KEY;
const errFd = fs.openSync(path.join(dir, "agent-stderr.log"), "w");
const agent = spawn(path.join(__dirname, "..", "agent"), [], { env, stdio: ["ignore", "ignore", errFd] });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let seq = 0;
async function msg(from, text) {
  seq++;
  const res = await fetch(`${BASE}/webhook`, { method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ object: "whatsapp_business_account", entry: [{ changes: [{ value: { messages: [{ from, id: "w" + seq, type: "text", text: { body: text } }] } }] }] }) });
  await res.text();
}
function outboxTo(who) {
  const f = path.join(dir, "data", "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => { try { return JSON.parse(l); } catch { return {}; } }).filter((r) => r.to === who);
}
const bodies = (rs) => rs.map((r) => r.body || "").join("\n");
async function say(from, text) {
  const before = outboxTo(from).length;
  await msg(from, text);
  let lastCount = -1, stable = 0;
  for (let i = 0; i < 160; i++) {
    await sleep(500);
    const n = outboxTo(from).length;
    if (n === lastCount) stable++; else { stable = 0; lastCount = n; }
    if (n > before && stable >= 3) break;
  }
  return outboxTo(from).slice(before);
}
const store = (name) => { const f = path.join(dir, "data", name); return fs.existsSync(f) ? JSON.parse(fs.readFileSync(f, "utf8")) : null; };
const orders = () => Object.values(store("orders.json") || {});
const hasButton = (who) => outboxTo(who).some((r) => r.kind === "cta_url");

async function buyHeels(buyer) {
  await say(buyer, "SHOP-DESK");
  await say(buyer, "Ankle Strap Heels");
  await say(buyer, "120");
  await say(buyer, "I will pick it up myself");
  await say(buyer, "Akosua Mensah");
  await say(buyer, "no");
  await say(buyer, "yes");
}

(async () => {
  let pass = 0, fail = 0;
  const check = (name, ok, extra) => { console.log((ok ? "PASS " : "FAIL ") + name + (extra ? " — " + extra : "")); ok ? pass++ : fail++; };
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }

    const emptyView = await say(M, "my orders");
    check("her orders list starts honest (only the seeded paid one shows)", bodies(emptyView).includes("DESK-0007") && bodies(emptyView).includes("paid, not sent yet"));

    // Sale one: stock is 1, so the gate must hold it for her word.
    await buyHeels(B1);
    const gated = !orders().some((o) => o.from === B1) && /in hand/i.test(bodies(outboxTo(B1)));
    check("with 1 in stock the sale waits for her in-hand word", gated);
    await say(M, "yes");
    for (let i = 0; i < 30 && !orders().some((o) => o.from === B1); i++) await sleep(600);
    const first = orders().find((o) => o.from === B1);
    check("her word completes it and the button goes out", !!first && hasButton(B1), first ? first.reference : "no order");

    // The list shows it the moment it lands.
    const view = await say(M, "my orders");
    check("my orders lists the new sale as waiting for payment", !!first && bodies(view).includes(first.reference) && bodies(view).includes("waiting for payment"));

    // She sets the count; the same sale then runs straight through.
    const stockReply = await say(M, "set stock of Ankle Strap Heels to 5");
    const shop = Object.values(store("catalogue.json").merchants)[0];
    check("set stock lands on the catalogue", bodies(stockReply).includes("now has 5 in stock") && shop.products[0].stock === 5);
    await buyHeels(B2);
    const second = orders().find((o) => o.from === B2);
    check("with 5 in stock the same sale needs no in-hand word", !!second && hasButton(B2), second ? second.reference : "no order");

    // She cancels the first order; everyone hears the truth.
    const dayBefore = (store("daybook.json") || {}).DESK || {};
    await say(M, "cancel order " + first.reference);
    const cancelled = orders().find((o) => o.reference === first.reference);
    check("the order reads cancelled in the book", !!cancelled && cancelled.status === "cancelled");
    check("the buyer is told at once, with nothing owed", /cancelled by the shop/.test(bodies(outboxTo(B1))) && /Nothing was paid/.test(bodies(outboxTo(B1))));
    const viewAfter = await say(M, "my orders");
    check("her list shows it as cancelled", bodies(viewAfter).includes(first.reference) && /cancelled/.test(bodies(viewAfter)));
    const dayAfter = (store("daybook.json") || {}).DESK || {};
    check("the day book gave the sale back", (dayAfter.revenue || 0) === (dayBefore.revenue || 0) - 120 && (dayAfter.sales || 0) === (dayBefore.sales || 0) - 1,
      "revenue " + dayBefore.revenue + " -> " + dayAfter.revenue);

    // A paid order she cancels names the refund, and the
    // buyer's dispute still opens on it.
    await say(M, "cancel order DESK-0007");
    check("the paid buyer is told the refund is owed", /owes you that money back/.test(bodies(outboxTo(B9))));
    await say(B9, "SHOP-DESK");
    await say(B9, "This is a scam. I paid for my heels, she cancelled the order and kept my money");
    const disputed = orders().find((o) => o.reference === "DESK-0007");
    check("her dispute opens on the cancelled, paid order", !!disputed && disputed.disputeState === "open");

    const errText = fs.readFileSync(path.join(dir, "agent-stderr.log"), "utf8");
    check("the agent never panicked", !/panic/i.test(errText));
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    try { agent.kill("SIGKILL"); } catch {}
    console.log(`\nshop desk proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
