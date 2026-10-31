// The merchant storm: idempotency and concurrency with
// the real model key, in the cast the owner named —
// five merchants and five buyers, all moving at once.
//
// Merchants, concurrently:
//   M1, M2 — existing shops adding a product each, in
//     the chat walk, while everything else runs;
//   M3, M4 — brand-new numbers onboarding from nothing:
//     open a shop, then stock its first product;
//   M5 — the receiving stall: her shop takes the buyers'
//     orders while she watches the notices land.
// Buyers, concurrently:
//   B1, B2 — buying from the same stall at the same time;
//   B3 — buying multiple products at that stall (basket);
//   B4 — buying multiple products at another stall, as
//     one declared total ("300 for all" over two picks);
//   B5 — reordering her last order at the stall.
// Idempotency drills, fired as duplicate webhooks with
// the SAME message id (what WhatsApp redelivery does):
//   D1 — B2's final confirm, twice at once: one order.
//   D2 — M1's save yes, twice at once: one product.
//   D3 — M3's shop-name answer, twice at once: one shop.
//   D4 — B4's second pick tap, twice at once: a toggle
//     must not be toggled back off by the redelivery.
// Afterwards the books are audited like an enemy: order
// references unique, each buyer's orders exactly what
// she made, totals reconciling item by item, the day
// books equal to the order books, the added and onboarded
// products standing exactly once, no cross-talk between
// shops, no panic, health green at the end.
// Run:
//   META_MODEL_API_KEY=<redacted> node scripts/prove-merchant-storm.js
"use strict";
const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

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

if (!process.env.META_MODEL_API_KEY) { console.log("META_MODEL_API_KEY required"); process.exit(1); }

const ROOT = path.join(__dirname, "..");
const PORT = 3501;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "mstorm-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const errLog = path.join(dir, "agent-stderr.log");

const OM = "233551310001";           // M5, the receiving stall's owner
const OA1 = "233551310002";          // M1
const OA2 = "233551310003";          // M2
const OO1 = "233551310004";          // M3, onboarding
const OO2 = "233551310005";          // M4, onboarding
const B1 = "233551310101", B2 = "233551310102", B3 = "233551310103", B4 = "233551310104", B5 = "233551310105";

fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({ merchants: {
  RECE: { id: "RECE", shopCode: "RECE", name: "Receiving Stall", ownerWhatsApp: OM, payoutPhone: OM,
    dailyGiveawayAllowance: 500, deliveryFee: 20, deliveryPaidTo: "merchant",
    products: [
      { id: "heels", name: "Ankle Strap Heels", description: "Pointed ankle-strap heels", listPrice: 120, floorPrice: 100, stock: 5 },
      { id: "clutch", name: "Clutch Bag", description: "Woven clutch bag", listPrice: 230, floorPrice: 200, stock: 5 },
      { id: "dress", name: "Halter Dress", description: "Halter-neck dress", listPrice: 150, floorPrice: 130, stock: 5 } ] },
  TOTA: { id: "TOTA", shopCode: "TOTA", name: "Total Shop", ownerWhatsApp: "233551310009", payoutPhone: "233551310009",
    dailyGiveawayAllowance: 500, deliveryFee: 20, deliveryPaidTo: "merchant",
    products: [
      { id: "rack", name: "Double Clothes Rack", description: "Black metal double clothes rack", listPrice: 270, floorPrice: 250, stock: 3 },
      { id: "scarf", name: "Silk Scarf", description: "Printed silk scarf", listPrice: 60, floorPrice: 50, stock: 3 } ] },
  ADD1: { id: "ADD1", shopCode: "ADD1", name: "Adds One", ownerWhatsApp: OA1, payoutPhone: OA1,
    dailyGiveawayAllowance: 300,
    products: [{ id: "soap", name: "Black Soap", description: "African black soap", listPrice: 25, floorPrice: 20, stock: 9 }] },
  ADD2: { id: "ADD2", shopCode: "ADD2", name: "Adds Two", ownerWhatsApp: OA2, payoutPhone: OA2,
    dailyGiveawayAllowance: 300,
    products: [{ id: "rice", name: "Perfumed Rice", description: "Long-grain perfumed rice", listPrice: 90, floorPrice: 80, stock: 9 }] },
} }));
// B5's last order at the stall: paid, delivered, hers to
// order again.
fs.writeFileSync(path.join(dir, "data", "orders.json"), JSON.stringify({
  "RECE-0001": { reference: "RECE-0001", merchantId: "RECE", from: B5, productName: "Clutch Bag", price: 230,
    total: 250, deliveryFee: 20, status: "paid", paymentPath: "seev", fulfilment: "delivery",
    location: "Osu", packageName: "Kojo Tsey", deliveryState: "received", createdAt: new Date().toISOString() },
}));

const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1" };
delete env.SEEV_SECRET_KEY; delete env.SEEV_API_KEY; delete env.SEEV_WEBHOOK_SECRET;
const errFd = fs.openSync(errLog, "w");
const agent = spawn(path.join(ROOT, "agent"), [], { env, stdio: ["ignore", "ignore", errFd] });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let seq = 0, httpErrors = 0;
const latencies = [];
async function post(message) {
  const res = await fetch(`${BASE}/webhook`, { method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ object: "whatsapp_business_account", entry: [{ changes: [{ value: { messages: [message] } }] }] }) });
  if (!res.ok) httpErrors++;
  await res.text();
}
async function msg(from, text, fixedId) { seq++; await post({ from, id: fixedId || "wm" + seq, type: "text", text: { body: text } }); }
async function tap(from, rowID) { seq++; await post({ from, id: "wm" + seq, type: "interactive", interactive: { type: "list_reply", list_reply: { id: rowID, title: "" } } }); }
function outboxTo(who) {
  const f = path.join(dir, "data", "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => { try { return JSON.parse(l); } catch { return {}; } }).filter((r) => r.to === who);
}
const bodies = (rs) => rs.map((r) => r.body || "").join("\n");
const hasPayButton = (who) => outboxTo(who).some((r) => r.kind === "cta_url" && (r.buttons || []).join(" ").includes("Click to pay"));
// say waits until the recipient's outbox settles: under
// the storm a turn queues behind the others, and the
// queueing is part of what is being measured.
async function say(from, text, fixedId) {
  const before = outboxTo(from).length;
  const t0 = Date.now();
  await msg(from, text, fixedId);
  let lastCount = -1, stable = 0;
  for (let i = 0; i < 260; i++) {
    await sleep(500);
    const n = outboxTo(from).length;
    if (n === lastCount) { stable++; } else { stable = 0; lastCount = n; }
    if (n > before && stable >= 3) break;
  }
  latencies.push(Date.now() - t0);
  return outboxTo(from).slice(before);
}
async function waitForText(who, phrase, capMs) {
  const t0 = Date.now();
  while (Date.now() - t0 < (capMs || 180000)) {
    if (bodies(outboxTo(who)).includes(phrase)) return true;
    await sleep(700);
  }
  return false;
}
// haggleToDeal offers, then offers the engine's own last
// figure back until the deal line appears.
async function haggleToDeal(buyer, opening) {
  let r = await say(buyer, opening);
  for (let i = 0; i < 5; i++) {
    if (bodies(outboxTo(buyer).slice(-6)).includes("yours for")) return true;
    const figs = [...bodies(r).matchAll(/GH₵([\d,]+)/g)].map((m) => parseFloat(m[1].replace(",", "")));
    if (!figs.length) return false;
    r = await say(buyer, String(figs[figs.length - 1]));
  }
  return bodies(outboxTo(buyer).slice(-6)).includes("yours for");
}
function orders() {
  const f = path.join(dir, "data", "orders.json");
  if (!fs.existsSync(f)) return [];
  return Object.values(JSON.parse(fs.readFileSync(f, "utf8")));
}
function store(name) {
  const f = path.join(dir, "data", name);
  if (!fs.existsSync(f)) return null;
  return JSON.parse(fs.readFileSync(f, "utf8"));
}
function catalogue() { return store("catalogue.json").merchants; }

// until polls a fact about the world (the catalogue on
// disk, an outbox phrase) until it holds or the cap runs
// out: under the storm a save can land a few seconds
// after its turn's replies settle.
async function until(fn, capMs) {
  const t0 = Date.now();
  while (Date.now() - t0 < (capMs || 90000)) {
    if (fn()) return true;
    await sleep(1500);
  }
  return fn();
}
// waitForAny polls the actor's whole outbox for the
// first of several phrases.
async function waitForAny(who, phrases, capMs) {
  const t0 = Date.now();
  while (Date.now() - t0 < (capMs || 240000)) {
    const all = bodies(outboxTo(who));
    for (const ph of phrases) if (all.includes(ph)) return ph;
    await sleep(700);
  }
  return "";
}
// finishWalk answers whatever the walk asks after the
// last price — and it is the walk, not the driver, that
// chooses: a product whose measure the read settles goes
// straight to the summary; an uncertain one is asked
// "by measure"; a measured one is asked how much of it
// the price covers. All three roads end at the summary.
async function finishWalk(owner, name, list, last) {
  const which = await waitForAny(owner, ["Here is what will go live", "by measure", "How much of it is the"], 300000);
  if (which === "by measure") {
    await say(owner, "only as a whole");
  } else if (which === "How much of it is the") {
    await say(owner, "6 yards");
  } else if (which === "") {
    throw new Error("the walk never reached its summary");
  }
  must(await until(() => bodies(outboxTo(owner)).includes(name) && bodies(outboxTo(owner)).includes("GH₵" + list) && bodies(outboxTo(owner)).includes("GH₵" + last), 300000), "the summary never came");
}
// The add-a-product walk, driven by its own questions:
// each answer waits for the question it answers, because
// under the storm a reply can queue for a minute and an
// answer sent on outbox-settle alone races ahead of the
// walk and lands in the wrong slot. (The walk itself
// re-asks rather than corrupting anything when that
// happens — the first run of this proof desynced two
// drivers exactly that way.)
async function addProduct(owner, name, desc, list, last) {
  await say(owner, "add a product");
  must(await waitForText(owner, "What is it called?", 300000), "the name question never came");
  await say(owner, name);
  must(await waitForText(owner, "Tell me about", 300000), "the description question never came");
  await say(owner, desc);
  must(await waitForText(owner, "list price", 300000), "the list price question never came");
  await say(owner, String(list));
  must(await waitForText(owner, "last price", 300000), "the last price question never came");
  await say(owner, String(last));
  await finishWalk(owner, name, list, last);
}
// Onboarding from nothing, then the first product, in the
// same sitting the signup hands over to the walk.
async function onboard(owner, shopName, name, desc, list, last, dupNameStep) {
  await say(owner, "I want to open a shop");
  must(await waitForText(owner, "what is your shop called", 300000), "the shop name question never came");
  if (dupNameStep) {
    // D3: the same answer delivered twice at once.
    await Promise.all([msg(owner, shopName, "wDUP-NAME"), msg(owner, shopName, "wDUP-NAME")]);
    await sleep(4000);
  } else {
    await say(owner, shopName);
  }
  must(await waitForText(owner, "What is it called?", 300000), "the product name question never came");
  await say(owner, name);
  must(await waitForText(owner, "Tell me about", 300000), "the description question never came");
  await say(owner, desc);
  must(await waitForText(owner, "list price", 300000), "the list price question never came");
  await say(owner, String(list));
  must(await waitForText(owner, "last price", 300000), "the last price question never came");
  await say(owner, String(last));
  await finishWalk(owner, name, list, last);
}

const results = { done: [], failed: [] };
async function journey(name, fn) {
  const t0 = Date.now();
  try { await fn(); results.done.push(name + " (" + ((Date.now() - t0) / 1000).toFixed(0) + "s)"); }
  catch (e) { results.failed.push(name + ": " + e.message); }
}
const must = (cond, what) => { if (!cond) throw new Error(what); };

(async () => {
  let pass = 0, fail = 0;
  const check = (name, ok, extra) => { console.log((ok ? "PASS " : "FAIL ") + name + (ok ? "" : " — " + (extra || ""))); ok ? pass++ : fail++; };
  const stormStart = Date.now();
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }

    // The receiving merchant watches her notices land.
    const notices = { count: 0 };
    let watching = true;
    const watcher = (async () => {
      let seen = 0;
      while (watching) {
        const all = outboxTo(OM);
        for (const r of all.slice(seen)) if ((r.body || "").includes("New order")) notices.count++;
        seen = all.length;
        await sleep(800);
      }
    })();

    const jobs = [
      journey("M1 adds a product", async () => {
        await addProduct(OA1, "Shea Butter", "Raw shea butter, whipped smooth.", 80, 70);
        // D2: the save yes delivered twice at once.
        await Promise.all([msg(OA1, "yes", "wDUP-SAVE"), msg(OA1, "yes", "wDUP-SAVE")]);
        await sleep(4000);
        must(await until(() => catalogue().ADD1.products.filter((p) => p.name === "Shea Butter").length === 1, 60000), "Shea Butter should stand exactly once");
      }),
      journey("M2 adds a product", async () => {
        await addProduct(OA2, "Groundnut Paste", "Stone-ground groundnut paste.", 45, 40);
        await say(OA2, "yes");
        must(await until(() => catalogue().ADD2.products.some((p) => p.name === "Groundnut Paste" && p.listPrice === 45 && p.floorPrice === 40), 60000), "Groundnut Paste not saved at her prices");
      }),
      journey("M3 onboards", async () => {
        await onboard(OO1, "Storm Fresh", "Palm Oil", "Red palm oil, cold pressed.", 60, 52, true);
        await say(OO1, "yes");
        const mine = Object.values(catalogue()).filter((m) => m.ownerWhatsApp === OO1);
        must(mine.length === 1 && mine[0].name === "Storm Fresh", "exactly one shop should stand for her number");
        must(await until(() => (Object.values(catalogue()).find((m) => m.ownerWhatsApp === OO1) || { products: [] }).products.some((p) => p.name === "Palm Oil" && p.listPrice === 60), 60000), "her first product should be stocked");
      }),
      journey("M4 onboards", async () => {
        await onboard(OO2, "Storm Prints", "Batik Cloth", "Hand-stamped batik cloth.", 200, 170, false);
        await say(OO2, "yes");
        const mine = Object.values(catalogue()).filter((m) => m.ownerWhatsApp === OO2);
        must(mine.length === 1 && mine[0].name === "Storm Prints", "exactly one shop should stand for her number");
        must(await until(() => (Object.values(catalogue()).find((m) => m.ownerWhatsApp === OO2) || { products: [] }).products.some((p) => p.name === "Batik Cloth" && p.listPrice === 200), 60000), "her first product should be stocked");
      }),
      journey("B1 buys from the stall", async () => {
        await say(B1, "SHOP-RECE");
        await say(B1, "Ankle Strap Heels");
        must(await haggleToDeal(B1, "120"), "no deal");
        await say(B1, "I will pick it up myself");
        await say(B1, "Akosua Mensah");
        await say(B1, "no");
        await say(B1, "yes");
        must(hasPayButton(B1), "no payment button");
      }),
      journey("B2 buys from the same stall", async () => {
        await say(B2, "SHOP-RECE");
        await say(B2, "Halter Dress");
        must(await haggleToDeal(B2, "150"), "no deal");
        await say(B2, "Delivery to Osu");
        await say(B2, "Efua Mensima");
        await say(B2, "no");
        // D1: the confirm delivered twice at once.
        await Promise.all([msg(B2, "yes", "wDUP-CONFIRM"), msg(B2, "yes", "wDUP-CONFIRM")]);
        await sleep(4000);
        must(hasPayButton(B2), "no payment button");
        must(orders().filter((o) => o.from === B2).length === 1, "the duplicated confirm should book exactly one order");
      }),
      journey("B3 buys multiple at the stall", async () => {
        await say(B3, "SHOP-RECE");
        await say(B3, "Ankle Strap Heels");
        must(await haggleToDeal(B3, "120"), "no deal on heels");
        await say(B3, "Add the Clutch Bag too");
        await say(B3, "yes");
        must(await haggleToDeal(B3, "230"), "no deal on the clutch");
        await say(B3, "I will pick it up myself");
        await say(B3, "Kojo Tsey");
        await say(B3, "no");
        await say(B3, "yes");
        must(hasPayButton(B3), "no payment button");
        const mine = orders().filter((o) => o.from === B3);
        must(mine.length === 1 && (mine[0].items || []).length === 2, "the basket should be one order of two items");
      }),
      journey("B4 buys multiple as one total", async () => {
        await say(B4, "SHOP-TOTA");
        await tap(B4, "shelf_rack");
        await sleep(2500);
        // D4: the second pick's tap delivered twice at
        // once — a redelivery must not toggle it off.
        seq++; const dupTap = { from: B4, id: "wDUP-TAP", type: "interactive", interactive: { type: "list_reply", list_reply: { id: "shelf_scarf", title: "" } } };
        await Promise.all([post(dupTap), post(dupTap)]);
        await sleep(4000);
        must(bodies(outboxTo(B4)).includes("2 items picked"), "both picks should be standing after the duplicated tap");
        must(await haggleToDeal(B4, "I will give you 310 for all"), "no deal on the total");
        await say(B4, "I will pick it up myself");
        await say(B4, "Ama Serwaa");
        await say(B4, "no");
        await say(B4, "yes");
        must(hasPayButton(B4), "no payment button");
        const mine = orders().filter((o) => o.from === B4);
        must(mine.length === 1 && (mine[0].items || []).length === 2, "the total should complete as one order of two items");
      }),
      journey("B5 reorders at the stall", async () => {
        await say(B5, "SHOP-RECE");
        await say(B5, "buy again");
        must(await waitForText(B5, "Kojo Tsey", 120000), "the reorder recap never came");
        await say(B5, "yes");
        await sleep(3000);
        if (!hasPayButton(B5)) { await say(B5, "yes"); }
        for (let i = 0; i < 120 && !hasPayButton(B5); i++) await sleep(700);
        must(hasPayButton(B5), "no payment button for the reorder");
        const mine = orders().filter((o) => o.from === B5 && o.reference !== "RECE-0001");
        must(mine.length === 1 && mine[0].price === 230 && mine[0].total === 250, "the reorder should repeat her last order at its own figures");
      }),
    ];
    await Promise.all(jobs);
    watching = false;
    await watcher;
    const stormSecs = ((Date.now() - stormStart) / 1000).toFixed(1);
    console.log("  journeys done: " + results.done.join(" | "));
    check(`all ${jobs.length} concurrent journeys completed (${stormSecs}s wall)`, results.failed.length === 0);
    if (results.failed.length) console.log("  journey failures: " + results.failed.join(" | "));

    // The receiving merchant, at the end of it.
    await say(OM, "what needs my attention today?");
    const receOrders = orders().filter((o) => o.merchantId === "RECE" && o.reference !== "RECE-0001");
    check("the stall's owner was told about her orders as they landed", notices.count >= receOrders.length, notices.count + " notices for " + receOrders.length + " orders");

    // The audit.
    const os_ = orders();
    const refs = os_.map((o) => o.reference);
    check("every order reference is unique", new Set(refs).size === refs.length);
    let figuresOK = true;
    for (const o of os_) {
      if (o.reference === "RECE-0001") continue;
      const itemsSum = (o.items && o.items.length) ? o.items.reduce((a, it) => a + it.price, 0) : o.price;
      const want = itemsSum + (o.fulfilment === "delivery" && o.merchantId !== "" ? (o.deliveryFee || 0) : 0);
      // Delivery at RECE and TOTA is merchant-paid, so a
      // delivery total carries the fee; pickups do not.
      if (o.total !== want && !(o.fulfilment !== "delivery" && o.total === itemsSum)) figuresOK = false;
    }
    check("every order's figures reconcile (total = items + its own fee where it applies)", figuresOK);
    const day = store("daybook.json") || {};
    let dayOK = true;
    for (const shop of ["RECE", "TOTA"]) {
      const goods = os_.filter((o) => o.merchantId === shop && o.reference !== "RECE-0001").reduce((n, o) => n + ((o.items && o.items.length) ? o.items.reduce((a, it) => a + it.price, 0) : o.price), 0);
      const booked = (day[shop] || {}).revenue || 0;
      if (Math.abs(booked - goods) > 0.001) dayOK = false;
    }
    check("the day books equal the order books, shop by shop", dayOK);
    const cat = catalogue();
    check("the added products stand exactly once each, at their prices",
      cat.ADD1.products.filter((p) => p.name === "Shea Butter" && p.listPrice === 80 && p.floorPrice === 70).length === 1 &&
      cat.ADD2.products.filter((p) => p.name === "Groundnut Paste").length === 1);
    check("the onboarded shops stand exactly once each, stocked",
      Object.values(cat).filter((m) => m.ownerWhatsApp === OO1).length === 1 &&
      Object.values(cat).filter((m) => m.ownerWhatsApp === OO2).length === 1);
    check("the receiving stall's shelf was not disturbed", cat.RECE.products.length === 3);
    let crossTalk = false;
    if (bodies(outboxTo(B1)).includes("Silk Scarf") || bodies(outboxTo(B1)).includes("Palm Oil")) crossTalk = true;
    if (bodies(outboxTo(B4)).includes("Clutch Bag")) crossTalk = true;
    if (bodies(outboxTo(OO1)).includes("Batik Cloth")) crossTalk = true;
    check("no actor ever saw another shop's goods", !crossTalk);
    check("the webhook never errored", httpErrors === 0);
    const errText = fs.existsSync(errLog) ? fs.readFileSync(errLog, "utf8") : "";
    check("the agent never panicked", !/panic/i.test(errText));
    const health = await fetch(`${BASE}/healthz`);
    check("the agent is still healthy at the end", health.ok);

    latencies.sort((a, b) => a - b);
    const p = (q) => latencies.length ? (latencies[Math.min(latencies.length - 1, Math.floor(q * latencies.length))] / 1000).toFixed(1) : "0";
    console.log(`  turns measured: ${latencies.length}, p50 ${p(0.5)}s, p95 ${p(0.95)}s, max ${latencies.length ? (latencies[latencies.length - 1] / 1000).toFixed(1) : 0}s`);
    console.log(`  orders on the books: ${os_.length} (incl. the seeded paid one)`);
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    try { agent.kill("SIGKILL"); } catch {}
    console.log(`\nmerchant storm: ${pass} pass, ${fail} fail (data: ${dir})`);
    process.exit(fail ? 1 : 0);
  }
})();

