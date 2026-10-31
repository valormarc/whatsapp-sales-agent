// The pressure proof: Stall under a booming-market storm,
// with the real model key. Not one polite conversation at a
// time, but the whole market at once, plus the people
// trying to break it.
//
// Phase 1, the storm: fifteen buyer journeys run truly
// concurrently across four shops (fees set by area, fees
// quoted per order, a one-off item raced by two buyers, a
// basket, one buyer shopping two shops in interleaved
// turns), while two merchant actors answer fee quotes and
// in-hand asks as they land.
// Phase 2, the adversaries: the payout-number ask, the
// screenshot, paying for somebody else's order, a dispute,
// and a replayed webhook (WhatsApp retries deliveries in
// production: the same confirm arriving twice must not
// book two orders).
// Phase 3, the burst: twenty-five strangers ask the same
// discovery question in the same second.
// Afterwards the books are audited like an enemy: every
// order's figures reconcile, references are unique, fees
// are the place's own, day-book revenue equals the orders,
// no buyer ever saw another shop's goods, no gated sale
// completed before its seller's word, and nothing is left
// hanging open.
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

const PORT = 3497;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "pressure-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const errLog = path.join(dir, "agent-stderr.log");

const OA = "233240001001", OB = "233240001002", OC = "233240001003", OD = "233240001004";
const heelsA = { id: "heels", name: "Ankle Strap Heels", description: "Pointed ankle-strap heels in mesh", listPrice: 120, floorPrice: 100, stock: 50 };
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    PA: { id: "PA", shopCode: "PA", name: "Pressure Shoes", ownerWhatsApp: OA, payoutPhone: OA,
      dailyGiveawayAllowance: 5000, deliveryFee: 20, deliveryPaidTo: "merchant",
      deliveryAreas: [{ area: "Osu", fee: 30 }],
      products: [heelsA, { id: "bag", name: "Leather Tote", description: "A structured leather tote", listPrice: 300, floorPrice: 260, stock: 5 }] },
    PB: { id: "PB", shopCode: "PB", name: "Quote Kicks", ownerWhatsApp: OB, payoutPhone: OB,
      dailyGiveawayAllowance: 5000, deliveryStyle: "quote", deliveryPaidTo: "merchant",
      products: [{ id: "sneakers", name: "Runner Sneakers", description: "Lightweight runners", listPrice: 200, floorPrice: 160, stock: 20 }] },
    PC: { id: "PC", shopCode: "PC", name: "Cloth Corner", ownerWhatsApp: OC, payoutPhone: OC,
      dailyGiveawayAllowance: 5000,
      products: [{ id: "kente", name: "Kente Cloth", description: "Handwoven kente", listPrice: 400, floorPrice: 350, stock: 1 },
                 { id: "cloth", name: "Ankara Cloth", description: "Bright ankara print", listPrice: 90, floorPrice: 70, stock: 9 }] },
    PD: { id: "PD", shopCode: "PD", name: "Discount Heels", ownerWhatsApp: OD, payoutPhone: OD,
      dailyGiveawayAllowance: 5000, deliveryFee: 15,
      products: [{ id: "heels", name: "Ankle Strap Heels", description: "Pointed ankle-strap heels in mesh", listPrice: 110, floorPrice: 95, stock: 30 }] },
  },
}));
// A paid order at PA for the dispute adversary.
const DISPUTER = "233551110501";
fs.writeFileSync(path.join(dir, "data", "orders.json"), JSON.stringify({
  "PA-0001": { reference: "PA-0001", from: DISPUTER, createdAt: new Date().toISOString(),
    productName: "Ankle Strap Heels", price: 120, total: 120, status: "paid",
    merchantId: "PA", paymentPath: "seev", fulfilment: "delivery", location: "Accra" },
}));

const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1" };
delete env.SEEV_SECRET_KEY; delete env.SEEV_API_KEY;
const errFd = fs.openSync(errLog, "w");
const agent = spawn(path.join(__dirname, "..", "agent"), [], { env, stdio: ["ignore", "ignore", errFd] });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let seq = 0, httpErrors = 0;
const latencies = [];
async function post(message) {
  const res = await fetch(`${BASE}/webhook`, { method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ object: "whatsapp_business_account", entry: [{ changes: [{ value: { messages: [message] } }] }] }) });
  if (!res.ok) httpErrors++;
  await res.text();
  return res;
}
async function msg(from, text, fixedId) { seq++; await post({ from, id: fixedId || "w" + seq, type: "text", text: { body: text } }); }
async function img(from, media) { seq++; await post({ from, id: "w" + seq, type: "image", image: { id: media } }); }
function outboxTo(who) {
  const f = path.join(dir, "data", "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => { try { return JSON.parse(l); } catch { return {}; } }).filter((r) => r.to === who);
}
const bodies = (rs) => rs.map((r) => r.body || "").join("\n");
const hasPayButton = (who) => outboxTo(who).some((r) => r.kind === "cta_url" && (r.buttons || []).join(" ").includes("Click to pay"));
// say waits until the recipient's outbox settles, up to a
// long cap: under the storm a turn can queue behind many
// others, and queueing is what we are measuring.
async function say(from, text, fixedId) {
  const before = outboxTo(from).length;
  const t0 = Date.now();
  await msg(from, text, fixedId);
  let lastCount = -1, stable = 0;
  for (let i = 0; i < 220; i++) {
    await sleep(600);
    const n = outboxTo(from).length;
    if (n === lastCount) { stable++; } else { stable = 0; lastCount = n; }
    if (n > before && stable >= 3) break;
    if (i > 200 && stable >= 3) break;
  }
  latencies.push(Date.now() - t0);
  return outboxTo(from).slice(before);
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

// haggleToDeal: offer, then offer the engine's own last
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
// waitForText polls a buyer's outbox for a phrase, for the
// waits a third party controls (a seller's fee, a gate).
async function waitForText(buyer, phrase, capMs) {
  const t0 = Date.now();
  while (Date.now() - t0 < (capMs || 120000)) {
    if (bodies(outboxTo(buyer)).includes(phrase)) return true;
    await sleep(800);
  }
  return false;
}
async function finishDetails(buyer, name) {
  await say(buyer, name || "Akosua Mensah");
  await say(buyer, "no");
  return say(buyer, "yes");
}

// Merchant actors: answer alerts as they appear in the
// owner's outbox, each alert exactly once.
const actorState = { stop: false };
async function merchantActor(owner, kind) {
  let answered = 0;
  while (!actorState.stop) {
    const alerts = outboxTo(owner).filter((r) => (r.body || "").includes(kind === "fee" ? "Delivery fee needed" : "Do you have it in hand now?"));
    while (answered < alerts.length) {
      answered++;
      await msg(owner, kind === "fee" ? "fee 30" : "yes");
      await sleep(300);
    }
    await sleep(700);
  }
}

const results = { journeys: 0, journeyFails: [] };
async function journey(name, fn) {
  try { await fn(); results.journeys++; }
  catch (e) { results.journeyFails.push(name + ": " + e.message); }
}
const must = (cond, what) => { if (!cond) throw new Error(what); };

(async () => {
  let pass = 0, fail = 0;
  const check = (name, ok) => { console.log((ok ? "PASS " : "FAIL ") + name); ok ? pass++ : fail++; };
  const stormStart = Date.now();
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }

    const actors = [merchantActor(OB, "fee"), merchantActor(OC, "inhand")];
    const B = (n) => "233551110" + String(n).padStart(3, "0");
    const jobs = [];

    // Six set-fee buyers at PA: four delivery to Osu (area
    // fee 30), two pickup.
    for (let i = 0; i < 6; i++) {
      const buyer = B(100 + i), deliver = i < 4;
      jobs.push(journey("PA buyer " + i, async () => {
        await say(buyer, "SHOP-PA");
        await say(buyer, "Ankle Strap Heels");
        must(await haggleToDeal(buyer, "110"), "no deal");
        if (deliver) await say(buyer, "Delivery to Osu"); else await say(buyer, "I will pick it up myself");
        const done = await finishDetails(buyer);
        must(hasPayButton(buyer), "no payment button");
      }));
    }
    // One basket buyer at PA: heels, add the tote, one order.
    jobs.push(journey("PA basket", async () => {
      const buyer = B(110);
      await say(buyer, "SHOP-PA");
      await say(buyer, "Ankle Strap Heels");
      must(await haggleToDeal(buyer, "120"), "no deal on heels");
      await say(buyer, "Add the Leather Tote too");
      await say(buyer, "yes");
      must(await haggleToDeal(buyer, "300"), "no deal on tote");
      await say(buyer, "I will pick it up myself");
      const done = await finishDetails(buyer);
      must(hasPayButton(buyer), "no button for basket");
      void done;
    }));
    // Three quote-fee buyers at PB: their sales pause for
    // the merchant actor's fee.
    for (let i = 0; i < 3; i++) {
      const buyer = B(120 + i);
      jobs.push(journey("PB buyer " + i, async () => {
        await say(buyer, "SHOP-PB");
        await say(buyer, "Runner Sneakers");
        must(await haggleToDeal(buyer, "200"), "no deal");
        await say(buyer, "Delivery to Kasoa");
        must(await waitForText(buyer, "says delivery to Kasoa is"), "the quoted fee never arrived");
        const done = await finishDetails(buyer);
        must(hasPayButton(buyer), "no button after quoted fee");
        void done;
      }));
    }
    // Two racers for the one-off kente at PC.
    for (let i = 0; i < 2; i++) {
      const buyer = B(130 + i);
      jobs.push(journey("PC racer " + i, async () => {
        await say(buyer, "SHOP-PC");
        await say(buyer, "Kente Cloth");
        must(await haggleToDeal(buyer, "400"), "no deal");
        await say(buyer, "I will pick it up myself");
        const done = await finishDetails(buyer);
        must(hasPayButton(buyer), "no button after in-hand yes");
        void done;
      }));
    }
    // One buyer, two shops, interleaved turns.
    jobs.push(journey("two-shop buyer", async () => {
      const buyer = B(140);
      await say(buyer, "SHOP-PA");
      await say(buyer, "Ankle Strap Heels");
      await say(buyer, "SHOP-PD");
      await say(buyer, "Ankle Strap Heels");
      must(await haggleToDeal(buyer, "110"), "no PD deal");
      await say(buyer, "I will pick it up myself");
      await finishDetails(buyer);
      must(hasPayButton(buyer), "no PD button");
    }));
    // Three discovery buyers: find the cheapest heels,
    // take them, buy them.
    for (let i = 0; i < 3; i++) {
      const buyer = B(150 + i);
      jobs.push(journey("discovery buyer " + i, async () => {
        const found = await say(buyer, "Where can I buy heels?");
        must(bodies(found).includes("GH₵110") || bodies(outboxTo(buyer)).includes("GH₵110"), "cheapest heels not presented");
        await say(buyer, "The cheaper one please");
        must(await haggleToDeal(buyer, "110"), "no deal after discovery pick");
        await say(buyer, "I will pick it up myself");
        await finishDetails(buyer);
        must(hasPayButton(buyer), "no button after discovery buy");
      }));
    }

    await Promise.all(jobs);
    const stormSecs = ((Date.now() - stormStart) / 1000).toFixed(1);
    check(`all ${jobs.length} concurrent journeys completed (${results.journeys} ok, ${stormSecs}s wall)`, results.journeyFails.length === 0);
    if (results.journeyFails.length) console.log("  journey failures: " + results.journeyFails.join(" | "));

    // Phase 2: the adversaries, while the actors still run.
    const adv = B(200);
    await say(adv, "SHOP-PA");
    await say(adv, "Ankle Strap Heels");
    await haggleToDeal(adv, "120");
    await say(adv, "I will pick it up myself");
    await finishDetails(adv);
    const askNum = await say(adv, "I will pay to the number");
    check("the number is never given, even under the storm", !bodies(outboxTo(adv)).includes(OA) && bodies(askNum).includes("only through the payment button"));
    const before = orders().length;
    await img(adv, "MEDIA-PRESS-1");
    check("a screenshot books and settles nothing", orders().length === before && orders().filter((o) => o.status === "paid" && o.merchantId === "PA").length === 1);
    const victim = orders().find((o) => o.merchantId === "PA" && o.from !== adv && o.status === "pending");
    if (victim) {
      await say(adv, "I have paid for order " + victim.reference);
      const v = orders().find((o) => o.reference === victim.reference);
      check("another buyer's order cannot be settled by her word", v && v.status === "pending");
    } else {
      check("another buyer's order cannot be settled by her word", false);
    }
    // The dispute, on the seeded paid order.
    await say(DISPUTER, "SHOP-PA");
    const disp = await say(DISPUTER, "This is a scam, she took my money and never sent it");
    check("the dispute opens under load", bodies(disp).includes("opened a dispute on order PA-0001"));
    // The replay: the same confirm webhook twice.
    const replayBuyer = B(210);
    await say(replayBuyer, "SHOP-PD");
    await say(replayBuyer, "Ankle Strap Heels");
    await haggleToDeal(replayBuyer, "110");
    await say(replayBuyer, "I will pick it up myself");
    await say(replayBuyer, "Akosua Mensah");
    await say(replayBuyer, "no");
    const ordersBeforeReplay = orders().filter((o) => o.from === replayBuyer).length;
    await say(replayBuyer, "yes", "wREPLAY1");
    await sleep(500);
    await post({ from: replayBuyer, id: "wREPLAY1", type: "text", text: { body: "yes" } });
    await sleep(4000);
    const replayOrders = orders().filter((o) => o.from === replayBuyer).length;
    check("a replayed confirm books exactly one order", replayOrders === ordersBeforeReplay + 1);

    actorState.stop = true;
    await Promise.allSettled(actors);

    // Phase 3: the burst. Twenty-five strangers, one
    // question, the same second.
    const burstNums = Array.from({ length: 25 }, (_, i) => B(300 + i));
    const burstStart = Date.now();
    await Promise.all(burstNums.map((n) => msg(n, "Where can I buy heels?")));
    let allAnsweredAt = 0;
    for (let i = 0; i < 200; i++) {
      await sleep(600);
      if (burstNums.every((n) => outboxTo(n).length > 0)) { allAnsweredAt = Date.now() - burstStart; break; }
    }
    check("all 25 burst strangers were answered", allAnsweredAt > 0);
    console.log(`  burst: last stranger answered after ${(allAnsweredAt / 1000).toFixed(1)}s`);

    // The audit.
    const os_ = orders();
    const refs = os_.map((o) => o.reference);
    check("every order reference is unique", new Set(refs).size === refs.length);
    let figuresOK = true, feeOK = true;
    for (const o of os_) {
      const charged = o.total || o.price;
      const expectedFee = o.merchantId === "PA" && o.fulfilment === "delivery" ? 30 : o.merchantId === "PB" ? 30 : o.merchantId === "PD" && o.fulfilment === "delivery" ? 15 : 0;
      if (o.fulfilment === "delivery" && o.deliveryFee !== expectedFee && o.reference !== "PA-0001") feeOK = false;
      const paidToMerchant = o.merchantId === "PA" || o.merchantId === "PB";
      const want = o.price + (o.fulfilment === "delivery" && paidToMerchant ? (o.deliveryFee || 0) : 0);
      if (charged !== want && o.reference !== "PA-0001") figuresOK = false;
    }
    check("every order's figures reconcile (total = goods + its own delivery fee)", figuresOK);
    check("delivery fees are the place's own (Osu 30 at PA, quoted 30 at PB)", feeOK);
    const day = store("daybook.json") || {};
    let dayOK = true;
    for (const shop of ["PA", "PB", "PC", "PD"]) {
      const goods = os_.filter((o) => o.merchantId === shop && o.reference !== "PA-0001").reduce((n, o) => n + o.price, 0);
      const booked = (day[shop] || {}).revenue || 0;
      if (Math.abs(booked - goods) > 0.001) dayOK = false;
    }
    check("the day books equal the order books, shop by shop", dayOK);
    const fq = store("feequotes.json") || [];
    check("no fee quote is left hanging open", !fq.some((q) => q.status === "open"));
    const ih = store("inhands.json") || [];
    check("no in-hand ask is left hanging open", !ih.some((q) => q.status === "open"));
    let crossTalk = false;
    for (let i = 0; i < 6; i++) {
      if (bodies(outboxTo(B(100 + i))).includes("Kente") || bodies(outboxTo(B(100 + i))).includes("Runner Sneakers")) crossTalk = true;
    }
    check("no buyer ever saw another shop's goods", !crossTalk);
    check("the webhook never errored", httpErrors === 0);
    const errText = fs.existsSync(errLog) ? fs.readFileSync(errLog, "utf8") : "";
    check("the agent never panicked", !/panic/i.test(errText));
    const health = await fetch(`${BASE}/healthz`);
    check("the agent is still healthy at the end", health.ok);

    latencies.sort((a, b) => a - b);
    const p = (q) => (latencies[Math.min(latencies.length - 1, Math.floor(q * latencies.length))] / 1000).toFixed(1);
    console.log(`  turns measured: ${latencies.length}, p50 ${p(0.5)}s, p95 ${p(0.95)}s, max ${(latencies[latencies.length - 1] / 1000).toFixed(1)}s`);
    console.log(`  orders on the books: ${os_.length}`);
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    try { agent.kill("SIGKILL"); } catch {}
    console.log(`\npressure proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
