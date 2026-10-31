#!/usr/bin/env node
// Live proof for the understanding lanes: buy again (same
// number; a stranger presenting the reference is refused,
// because the recap recites the buyer's own details and a
// serial is guessable; and through a quote shop's fresh
// fee), same details adopted whole, and take it at the
// price. Real model, dry-run WhatsApp, placeholder payment
// rail. Run from the repo root with the agent binary built.
const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");
const net = require("net");

// Sandbox hygiene: a dry-run child must never inherit live
// send or settle credentials from the operator's shell. The
// agent's sandbox exception is bound to a send-incapable
// process, so the live WhatsApp and Seev credentials are
// blanked here, before any child env is built from this one.
delete process.env.WHATSAPP_TOKEN;
delete process.env.WHATSAPP_PHONE_NUMBER_ID;
delete process.env.WHATSAPP_APP_SECRET;
delete process.env.WHATSAPP_VERIFY_TOKEN;
delete process.env.SEEV_SECRET_KEY;
delete process.env.SEEV_API_KEY;
delete process.env.SEEV_WEBHOOK_SECRET;

const ROOT = path.join(__dirname, "..");
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "reorder-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000401", B1 = "233240000402", B2 = "233240000403", B3 = "233240000404";
const catalogue = { merchants: {
  desk: { id: "desk", name: "Desk Heels", shopCode: "DESK", ownerWhatsApp: OWNER, payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 20, deliveryPaidTo: "merchant", deliveryStyle: "set",
    products: [{ id: "heels", name: "Ankle Strap Heels", description: "Pointed ankle-strap heels.", listPrice: 120, floorPrice: 100, stock: 5 }] },
  quot: { id: "quot", name: "Quot Kicks", shopCode: "QUOT", ownerWhatsApp: OWNER, payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryStyle: "quote",
    products: [{ id: "sneakers", name: "Runner Sneakers", description: "Lightweight runners.", listPrice: 200, floorPrice: 160, stock: 5 }] },
} };
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify(catalogue));
const past = (ref, shop, buyer, product, price, fee, place) => ({ reference: ref, merchantId: shop, from: buyer, productName: product, price, total: price + fee, deliveryFee: fee, status: "paid", paymentPath: "seev", fulfilment: "delivery", location: place, packageName: "Kojo Tsey", deliveryState: "received", createdAt: new Date().toISOString() });
fs.writeFileSync(path.join(dir, "data", "orders.json"), JSON.stringify({
  "DESK-0007": past("DESK-0007", "desk", B1, "Ankle Strap Heels", 120, 20, "Osu"),
  "QUOT-0003": past("QUOT-0003", "quot", B1, "Runner Sneakers", 200, 30, "Kasoa"),
}));

const freePort = () => new Promise((res, rej) => { const s = net.createServer(); s.once("error", rej); s.listen(0, () => { const p = s.address().port; s.close(() => res(p)); }); });

let seq = 0, passed = 0, failed = 0;
const seen = new Map();
function outbox(to) {
  const f = path.join(dir, "data", "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").split("\n").filter(Boolean).map((l) => { try { return JSON.parse(l); } catch { return null; } }).filter((r) => r && r.to === to);
}
function fresh(to) { const all = outbox(to); const n = seen.get(to) || 0; seen.set(to, all.length); return all.slice(n); }
// All the text a phone would show for a record: the body,
// any button titles, and a list sheet's row titles and
// descriptions. The shelf arrives as a list, so a check
// that waits for a product name must read its rows too —
// body alone never sees them.
const body = (rs) => rs.map((r) => [
  r.body || "",
  ...(r.buttons || []),
  ...((r.sections || []).flatMap((s) => (s.rows || []).flatMap((row) => [row.title || "", row.description || ""]))),
].join("\n")).join("\n");
const hasButton = (rs) => rs.some((r) => r.kind === "cta_url");
async function waitFor(to, pred, what, timeout = 150000) {
  const start = Date.now(); let acc = [];
  while (Date.now() - start < timeout) {
    acc = acc.concat(fresh(to));
    if (pred(body(acc), acc)) return acc;
    await new Promise((r) => setTimeout(r, 1500));
  }
  throw new Error("timeout waiting for " + what + "; got: " + body(acc).slice(0, 400));
}
function check(name, ok, extra = "") {
  if (ok) { passed++; console.log("  ok  " + name); }
  else { failed++; console.log("FAIL  " + name + (extra ? "  [" + String(extra).slice(0, 220) + "]" : "")); }
}
async function main() {
  const port = await freePort();
  const base = "http://127.0.0.1:" + port;
  const env = { ...process.env, WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1", HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), PORT: String(port) };
  delete env.SEEV_API_KEY; delete env.SEEV_SECRET_KEY; delete env.SEEV_WEBHOOK_SECRET;
  const agent = spawn(path.join(ROOT, "agent"), [], { cwd: dir, env, stdio: ["ignore", "pipe", "pipe"] });
  let agentLog = "";
  agent.stdout.on("data", (d) => { agentLog += d; });
  agent.stderr.on("data", (d) => { agentLog += d; });
  const post = async (from, text, extra = {}) => {
    const msg = { from, id: "w" + ++seq, timestamp: String(Math.floor(Date.now() / 1000)), type: "text", text: { body: text }, ...extra };
    const res = await fetch(base + "/webhook", { method: "POST", headers: { "content-type": "application/json" },
      body: JSON.stringify({ entry: [{ changes: [{ value: { messages: [msg] } }] }] }) });
    await res.text();
  };
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(base + "/healthz"); if (r.ok) break; } catch {} await new Promise((r) => setTimeout(r, 500)); }

    console.log("A. buy again on the same number");
    await post(B1, "SHOP-DESK"); await waitFor(B1, (t) => t.includes("Ankle"), "shelf");
    await post(B1, "Can I buy this again?");
    let rs = await waitFor(B1, (t) => t.includes("Kojo Tsey"), "the reorder recap");
    check("one recap carries the same details", body(rs).includes("Same again") && body(rs).includes("Osu"), body(rs));
    await post(B1, "Yes");
    rs = await waitFor(B1, (t, all) => hasButton(all), "the pay button");
    check("one yes reaches the pay button", true);
    let orders = JSON.parse(fs.readFileSync(path.join(dir, "data", "orders.json"), "utf8"));
    let mine = Object.values(orders).filter((o) => o.from === B1 && o.merchantId === "desk" && o.reference !== "DESK-0007");
    check("the repeat order stands with the old details", mine.length === 1 && mine[0].packageName === "Kojo Tsey" && mine[0].price === 120 && mine[0].deliveryFee === 20, JSON.stringify(mine));

    console.log("B. the price moved; a stranger presents the order and is refused; its owner is served");
    await post(OWNER, "change price of Ankle Strap Heels to 130");
    await waitFor(OWNER, (t) => t.includes("130"), "the price change");
    await post(B2, "SHOP-DESK"); await waitFor(B2, (t) => t.includes("Ankle"), "shelf");
    await post(B2, "This is my other number. My order was DESK-0007, can I repurchase it?");
    rs = await waitFor(B2, (t) => t.includes("different number"), "the refusal");
    check("a foreign reference gets the neutral answer", !body(rs).includes("Same again") && !body(rs).includes("Kojo Tsey") && !body(rs).includes("Osu"), body(rs));
    orders = JSON.parse(fs.readFileSync(path.join(dir, "data", "orders.json"), "utf8"));
    mine = Object.values(orders).filter((o) => o.from === B2);
    check("no order is built for the stranger", mine.length === 0, JSON.stringify(mine));
    await post(B1, "I want this order again DESK-0007");
    rs = await waitFor(B1, (t) => t.includes("GH₵130 now") || t.includes("Same again"), "the moved-price reorder");
    check("the moved price is named before she confirms", body(rs).includes("was GH₵120") && body(rs).includes("GH₵130 now"), body(rs));
    await post(B1, "Yes");
    await waitFor(B1, (t, all) => hasButton(all), "the pay button");
    orders = JSON.parse(fs.readFileSync(path.join(dir, "data", "orders.json"), "utf8"));
    mine = Object.values(orders).filter((o) => o.from === B1 && o.merchantId === "desk" && o.price === 130);
    check("the owner's repeat stands at today's price", mine.length === 1, JSON.stringify(mine));

    console.log("C. take it at the price");
    await post(B3, "SHOP-DESK"); await waitFor(B3, (t) => t.includes("Ankle"), "shelf");
    await post(B3, "Ankle Strap Heels"); await waitFor(B3, (t) => t.includes("GH₵130"), "the view");
    await post(B3, "I will take it at 130");
    rs = await waitFor(B3, (t) => t.includes("is yours for GH₵130"), "the instant deal");
    check("a full-price take closes at once, no haggle", !body(rs).includes("What do you say?"), body(rs));

    console.log("D. same details inside a fresh purchase");
    await post(B1, "Ankle Strap Heels"); await waitFor(B1, (t) => t.includes("GH₵130"), "the view");
    await post(B1, "I will take it at 130"); await waitFor(B1, (t) => /Same again, or different/.test(t), "the usual question");
    await post(B1, "Different"); await waitFor(B1, (t) => /How would you like|Delivery/.test(t), "the choice");
    await post(B1, "Delivery"); await waitFor(B1, (t) => /where|place/i.test(t), "the place ask");
    await post(B1, "Osu"); await waitFor(B1, (t) => t.includes("What name should go on the package?"), "the name ask");
    await post(B1, "Use the same details as last time");
    rs = await waitFor(B1, (t) => t.includes("Kojo Tsey"), "the recap with her name");
    check("her instruction is applied whole, never re-asked", !body(rs).includes("message for the seller"), body(rs));
    await post(B1, "Yes");
    await waitFor(B1, (t, all) => hasButton(all), "the pay button");

    console.log("E. buy again through a quote shop's fresh fee");
    await post(B1, "SHOP-QUOT"); await waitFor(B1, (t) => t.includes("Runner"), "the second shelf");
    await post(B1, "Same order again please");
    rs = await waitFor(B1, (t) => t.includes("asked"), "the fee being asked");
    check("the repeat pauses only for the fresh fee", body(rs).includes("Same again"), body(rs));
    let answered = false;
    for (let i = 0; i < 40 && !answered; i++) {
      const ownerRs = fresh(OWNER);
      if (body(ownerRs).includes("Delivery fee needed")) { await post(OWNER, "fee 30"); answered = true; break; }
      await new Promise((r) => setTimeout(r, 1500));
    }
    check("the seller was asked for the fee", answered);
    rs = await waitFor(B1, (t) => t.includes("Kojo Tsey") && t.includes("Kasoa"), "the parked recap");
    check("the fee lands on the parked details, straight to the recap", !body(rs).includes("What name should go on the package?"), body(rs));
    await post(B1, "Yes");
    await waitFor(B1, (t, all) => hasButton(all), "the pay button");
    orders = JSON.parse(fs.readFileSync(path.join(dir, "data", "orders.json"), "utf8"));
    mine = Object.values(orders).filter((o) => o.from === B1 && o.merchantId === "quot" && o.reference !== "QUOT-0003");
    check("the quote-shop repeat is booked with its fee", mine.length === 1 && mine[0].deliveryFee === 30 && mine[0].packageName === "Kojo Tsey", JSON.stringify(mine));

    check("no panics in the agent log", !/panic/i.test(agentLog));
  } catch (e) {
    failed++;
    console.log("FAIL  harness: " + e.message);
    console.log(agentLog.slice(-800));
  } finally {
    agent.kill("SIGTERM");
    console.log(`\nprove-reorder: ${passed} passed, ${failed} failed  (data: ${dir})`);
    process.exit(failed ? 1 : 0);
  }
}
main();
