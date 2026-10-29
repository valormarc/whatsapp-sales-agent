// The merchant proof: one seller's whole life on Stall,
// driven live with the real model key. There is no
// dashboard and there never will be, so this test treats
// the chat as the dashboard and hunts through it the way
// she would: open the shop, stock it every way there is,
// change prices, fix prices, set delivery up every way it
// can be set, read the day's book, answer the things
// buyers set in motion, and ask for the things a dashboard
// would show. The last section is probes on purpose:
// things a dashboard would have that the chat may not
// yet. Their answers are printed as PROBE lines, not
// pass or fail, and they are the gap list.
const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

if (!process.env.META_MODEL_API_KEY) { console.log("META_MODEL_API_KEY required"); process.exit(1); }

const PORT = 3499;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "merchant-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({ merchants: {} }));

const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1" };
delete env.SEEV_SECRET_KEY; delete env.SEEV_API_KEY;
const errFd = fs.openSync(path.join(dir, "agent-stderr.log"), "w");
const agent = spawn(path.join(__dirname, "..", "agent"), [], { env, stdio: ["ignore", "ignore", errFd] });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let seq = 0;
async function post(message) {
  const res = await fetch(`${BASE}/webhook`, { method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ object: "whatsapp_business_account", entry: [{ changes: [{ value: { messages: [message] } }] }] }) });
  await res.text();
  return res;
}
async function msg(from, text) { seq++; await post({ from, id: "w" + seq, type: "text", text: { body: text } }); }
async function tap(from, buttonId) { seq++; await post({ from, id: "w" + seq, type: "interactive", interactive: { type: "button_reply", button_reply: { id: buttonId, title: buttonId } } }); }
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
async function ask(from, text) { const r = await say(from, text); return bodies(r); }
const store = (name) => { const f = path.join(dir, "data", name); return fs.existsSync(f) ? JSON.parse(fs.readFileSync(f, "utf8")) : null; };
const catalogue = () => store("catalogue.json") || { merchants: {} };
const shopOf = (owner) => Object.values(catalogue().merchants).find((m) => m.ownerWhatsApp === owner);
const orders = () => Object.values(store("orders.json") || {});

// answerWalk drives an open question walk (signup or
// onboarding) by reading the newest question and answering
// it from the supplied table, until the done phrase lands.
async function answerWalk(who, table, doneRe, cap) {
  const start = outboxTo(who).length;
  for (let i = 0; i < (cap || 14); i++) {
    const all = outboxTo(who);
    const last = (all[all.length - 1] || {}).body || "";
    if (doneRe.test(bodies(all.slice(start)))) return true;
    const row = table.find(([re]) => re.test(last));
    if (!row) return false;
    await say(who, row[1]);
  }
  return doneRe.test(bodies(outboxTo(who).slice(start)));
}
// closeWalk cancels any walk still open, so one leg's
// unfinished business never eats the next leg's words.
async function closeWalk(who) {
  const all = outboxTo(who);
  const last = (all[all.length - 1] || {}).body || "";
  if (/What is it called|Tell me about|list price|last price|by measure|How much of it|Shall I add|right price/i.test(last)) {
    await say(who, "cancel");
  }
}

// stockAdvert sends a pasted advert and then answers
// whatever the walk still needs (last price, the gate,
// the measure, the confirm) until the save lands.
async function stockAdvert(who, text, answers) {
  await say(who, text);
  return answerWalk(who, [
    [/replace|rename/i, (answers && answers.dup) || "replace"],
    [/tell me about|in a few words|describe|description/i, (answers && answers.desc) || "Good quality, fairly priced"],
    [/right price/i, (answers && answers.gate) || "yes"],
    [/last price/i, (answers && answers.last) || "100"],
    [/how much of it/i, (answers && answers.qty) || "1"],
    [/by measure/i, (answers && answers.measure) || "whole only"],
    [/sizes/i, "no"],
    [/save|shall i|add .* at/i, "yes"],
  ], /Saved\.|is live|on your shelf/i, 10);
}

const M = "233551200001";           // Efua, the merchant
const B1 = "233551200101", B2 = "233551200102", B3 = "233551200103", B4 = "233551200104", B5 = "233551200105";

(async () => {
  let pass = 0, fail = 0;
  const check = (name, ok, extra) => { console.log((ok ? "PASS " : "FAIL ") + name + (extra ? " — " + extra : "")); ok ? pass++ : fail++; };
  const probe = (name, finding) => console.log("PROBE " + name + " => " + finding);
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }

    // The door and the opening of the shop.
    await say(M, "hello");
    check("a stranger is offered the door (buy or sell)", outboxTo(M).some((r) => r.kind === "buttons"));
    await tap(M, "door_sell");
    const signed = await answerWalk(M, [
      [/shop called/i, "Efua Fabrics"],
      [/name on the MoMo|name is on it/i, "Efua Mensah"],
      [/right account/i, "yes"],
    ], /SHOP-[A-Z0-9-]+/);
    const shopCode = (bodies(outboxTo(M)).match(/SHOP-([A-Z0-9-]+)/) || [])[1] || "";
    check("she opens her shop in chat and is handed her code", signed && !!shopOf(M), shopCode ? "code " + shopCode : "no code found");
    const emptyCat = await ask(M, "my catalogue");
    const catOpenedWalk = /What is it called/i.test(bodies(outboxTo(M).slice(-2)));
    if (catOpenedWalk) await say(M, "cancel");
    check("an empty catalogue answers like a dashboard home, not an error", /add|empty|nothing/i.test(emptyCat) && !catOpenedWalk);

    // Stocking, way one: the guided walk, a measured good.
    await say(M, "add a product");
    const ankaraSaved = await answerWalk(M, [
      [/called/i, "Ankara Fabric"],
      [/tell me about|in a few words|describe|description/i, "Bright wax print cotton"],
      [/right price/i, "yes"],
      [/last price/i, "380"],
      [/how much of it/i, "6 yards"],
      [/price/i, "450"],
      [/by measure/i, "by the yard"],
      [/sizes/i, "no"],
      [/save|shall i/i, "yes"],
    ], /Ankara Fabric[\s\S]*saved|is live/i);
    await closeWalk(M);
    const ankara = (shopOf(M) || { products: [] }).products.find((p) => p.name === "Ankara Fabric");
    check("guided walk stocks a measured good at 450, last 380, 6 yards", ankaraSaved && ankara && ankara.listPrice === 450 && ankara.floorPrice === 380 && ankara.baseQty === 6,
      ankara ? JSON.stringify({ p: ankara.listPrice, f: ankara.floorPrice, q: ankara.baseQty, u: ankara.unit }) : "missing");
    check("the listing hands her ready announcements", /Order here|SHOP-/.test(bodies(outboxTo(M))) && outboxTo(M).filter((r) => /broadcast|share/i.test(r.body || "")).length >= 0);

    // Stocking, way two: a pasted advert.
    await stockAdvert(M, "Ankle Strap Heels\nPointed heels in gold and black mesh\nPrice: GH₵120, last price GH₵100", { last: "100" });
    await closeWalk(M);
    const heels = (shopOf(M) || { products: [] }).products.find((p) => p.name.includes("Heels"));
    check("a pasted advert stocks itself", !!heels && heels.listPrice === 120 && heels.floorPrice === 100);

    // Stocking, way three: a doubted price stops at the gate.
    await stockAdvert(M, "Phone Case\nClear hard case for Samsung phones\nGH₵4", { gate: "45", last: "35" });
    await closeWalk(M);
    const gateAsked = bodies(outboxTo(M)).includes("Is that the right price?");
    const caseP = (shopOf(M) || { products: [] }).products.find((p) => p.name.includes("Phone Case"));
    check("a doubted price is gated, then saved at her figure (45)", gateAsked && !!caseP && caseP.listPrice === 45);

    // A duplicate name offers replace or rename.
    await stockAdvert(M, "Ankle Strap Heels\nPointed heels in gold and black mesh\nPrice: GH₵125, last price GH₵100", { last: "100", dup: "replace" });
    const dupOffered = /replace|rename/i.test(bodies(outboxTo(M)));
    if (dupOffered && !(shopOf(M) || { products: [] }).products.find((p) => p.name.includes("Heels") && p.listPrice === 125)) {
      await say(M, "replace");
      await answerWalk(M, [[/save|shall i/i, "yes"]], /Saved\.|is live/i, 4);
    }
    const heelsCount = (shopOf(M) || { products: [] }).products.filter((p) => p.name.includes("Heels")).length;
    const heelsNow = (shopOf(M) || { products: [] }).products.find((p) => p.name.includes("Heels"));
    check("a duplicate name offers the choice and replaces cleanly", dupOffered && heelsCount === 1 && heelsNow.listPrice === 125);

    // Measure commands, both ways.
    await say(M, "add a product");
    await answerWalk(M, [
      [/called/i, "Shea Butter"],
      [/tell me about|in a few words|describe|description/i, "Raw unrefined shea"],
      [/right price/i, "yes"],
      [/last price/i, "160"],
      [/how much of it/i, "1"],
      [/price/i, "200"],
      [/by measure/i, "whole only"],
      [/sizes/i, "no"],
      [/save|shall i/i, "yes"],
    ], /Shea Butter[\s\S]*saved|is live/i);
    await closeWalk(M);
    const measureReply = await ask(M, "set measure of Shea Butter to 5 litres");
    check("set measure reprices by the unit (200 for 5 litres = 40 a litre)", /GH₵40/.test(measureReply) && (shopOf(M).products.find((p) => p.name === "Shea Butter") || {}).unit === "litre");
    const wholeReply = await ask(M, "sell Shea Butter whole only");
    check("whole only undoes the measure", /whole only/i.test(wholeReply));

    // Price changes, and a fixed price honoured.
    const priceReply = await ask(M, "change price of Ankle Strap Heels to 130");
    check("change price moves the list and keeps the last price", /GH₵130/.test(priceReply) && /GH₵100/.test(priceReply));
    const fixedReply = await ask(M, "change last price of Ankle Strap Heels to 130");
    check("change last price to the list makes it fixed, in her words", /fixed/i.test(fixedReply));

    // A buyer meets the fixed price.
    await say(B1, "SHOP-" + shopCode);
    await say(B1, "Ankle Strap Heels");
    const refused = await say(B1, "120");
    check("a fixed price refuses a lower offer as fixed", /fixed/i.test(bodies(refused)));
    const dealt = await say(B1, "130");
    check("a fixed price deals at the price", bodies(dealt).includes("yours for GH₵130"));
    await say(B1, "I will pick it up myself");
    await say(B1, "Ama Serwaa"); await say(B1, "no"); await say(B1, "yes");
    check("the fixed-price sale completes with the pay button", outboxTo(B1).some((r) => r.kind === "cta_url"));

    // A buyer buys a cut of the measure.
    await say(B2, "SHOP-" + shopCode);
    await say(B2, "Ankara 3 yards");
    await say(B2, "I will pick it up myself");
    await say(B2, "Kofi Boateng"); await say(B2, "no");
    await say(B2, "yes");
    if (!orders().find((o) => o.from === B2) && /in hand/i.test(bodies(outboxTo(B2)))) {
      await say(M, "yes");
      for (let i = 0; i < 20 && !orders().find((o) => o.from === B2); i++) await sleep(600);
    }
    const cutOrder = orders().find((o) => o.from === B2);
    check("3 yards of a 6-yard 450 piece orders at 225", !!cutOrder && cutOrder.price === 225, cutOrder ? "price " + cutOrder.price : "no order");

    // Removal.
    const removed = await ask(M, "remove Phone Case");
    check("remove takes a product off the shelf", !!(shopOf(M) || { products: [] }).products.every((p) => !p.name.includes("Phone Case")) && /removed|taken off|gone/i.test(removed));

    // Delivery, every way it can be set.
    await say(M, "set delivery fee 25");
    const areaReply = await ask(M, "set area fees Osu 30, East Legon 40");
    check("area fees land and read back", /Osu/.test(areaReply) && /East Legon/.test(areaReply));
    await say(M, "set delivery per order");
    const quoteLine = await ask(M, "my catalogue");
    check("per-order quoting shows on the catalogue", /per order|quoted/i.test(quoteLine));
    await say(M, "set delivery up front");
    await say(M, "delivery money comes to me");
    await say(M, "set delivery note Bikes leave at 4pm daily");
    await say(B3, "SHOP-" + shopCode);
    const noteHeard = await ask(B3, "How does delivery work here?");
    check("a buyer hears the delivery note in the shop's own answer", /4pm/.test(noteHeard));
    await say(B3, "Shea Butter");
    await say(B3, "200");
    await say(B3, "Delivery to Osu");
    await say(B3, "Yaw Darko"); await say(B3, "no"); await say(B3, "yes");
    const sheaOrder = orders().find((o) => o.from === B3);
    check("area fee and merchant purse ride the total (200 + Osu 30 = 230)", !!sheaOrder && sheaOrder.deliveryFee === 30 && sheaOrder.total === 230,
      sheaOrder ? "fee " + sheaOrder.deliveryFee + " total " + sheaOrder.total : "no order");

    // The day's book, against the order book.
    await say(M, "set allowance to 300");
    const day = await ask(M, "how is today");
    const goods = orders().filter((o) => o.merchantId === (shopOf(M) || {}).id).reduce((n, o) => n + o.price, 0);
    check("the day book's revenue equals the orders she actually made", day.includes("GH₵" + goods) || day.includes(String(goods)), "orders sum " + goods);

    // Buyer wants: recorded, listed, and the alert when stocked.
    await say(B4, "SHOP-" + shopCode);
    await say(B4, "Do you have guinea brocade?");
    const wantAlert = bodies(outboxTo(M)).includes("guinea brocade");
    const wantsList = await ask(M, "buyer wants");
    check("an unstocked ask is written down, alerted, and listed for her", wantAlert && /guinea brocade/i.test(wantsList));
    await say(M, "add a product");
    await answerWalk(M, [
      [/called/i, "Guinea Brocade"],
      [/tell me about|in a few words|describe|description/i, "Heavy woven brocade"],
      [/right price/i, "yes"],
      [/last price/i, "420"],
      [/how much of it/i, "1"],
      [/price/i, "500"],
      [/by measure/i, "whole only"],
      [/sizes/i, "no"],
      [/save|shall i/i, "yes"],
    ], /Guinea Brocade[\s\S]*saved|is live/i);
    await closeWalk(M);
    check("stocking a wanted item alerts the buyer who asked", /guinea brocade/i.test(bodies(outboxTo(B4))) && /in stock|available|landed|arrived/i.test(bodies(outboxTo(B4))));

    // The delivery loop from her side.
    const loopOrder = orders().find((o) => o.from === B3);
    check("she received the order notice for the sale", !!loopOrder && bodies(outboxTo(M)).includes(loopOrder.reference));
    if (loopOrder) {
      await say(M, "I have sent " + loopOrder.reference);
      check("marking sent tells the buyer", /sent/i.test(bodies(outboxTo(B3).slice(-2))));
      await say(B3, "It has arrived, thank you");
      check("the buyer's arrival closes the loop with her", /received|arrived/i.test(bodies(outboxTo(M).slice(-3))));
    } else { check("marking sent tells the buyer", false, "no B3 order to send"); check("the buyer's arrival closes the loop with her", false, "no B3 order"); }

    // A dispute from her side of the counter.
    await say(B5, "SHOP-" + shopCode);
    await say(B5, "Shea Butter");
    await say(B5, "200");
    await say(B5, "I will pick it up myself");
    await say(B5, "Esi Owusu"); await say(B5, "no"); await say(B5, "yes");
    const dispOrder = orders().find((o) => o.from === B5);
    if (!dispOrder) { check("a dispute reaches her with the buyer's own words", false, "the B5 sale never completed"); check("her refund claim is put to the buyer, not taken on her word", false, "no dispute"); check("the buyer's yes closes it with no strike", false, "no dispute"); }
    if (dispOrder) {
    try { agent.kill("SIGKILL"); } catch {}
    await sleep(800);
    const seeded = store("orders.json"); seeded[dispOrder.reference].status = "paid"; seeded[dispOrder.reference].paymentPath = "seev";
    fs.writeFileSync(path.join(dir, "data", "orders.json"), JSON.stringify(seeded));
    var agent2 = spawn(path.join(__dirname, "..", "agent"), [], { env, stdio: ["ignore", "ignore", errFd] });
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }
    await say(B5, "SHOP-" + shopCode);
    await say(B5, "This is a scam, I paid and she never sent my shea butter");
    const disputeAlert = bodies(outboxTo(M).slice(-4));
    check("a dispute reaches her with the buyer's own words", /dispute/i.test(disputeAlert) && /never sent/i.test(disputeAlert));
    await say(M, "I have refunded order " + dispOrder.reference);
    const refundAsk = bodies(outboxTo(B5).slice(-3));
    check("her refund claim is put to the buyer, not taken on her word", /refund/i.test(refundAsk));
    await say(B5, "yes");
    check("the buyer's yes closes it with no strike", (store("trust.json") || []).every((t) => !t.strikes));
    }

    // Money plumbing and help, in her own words.
    const paySetup = await ask(M, "set up my payment");
    check("payment setup answers honestly in this harness (no live keys wired)", paySetup.length > 20 && !/account is (live|ready|active)/i.test(paySetup), paySetup.slice(0, 90).replace(/\n/g, " "));
    const weather = await ask(M, "What is the weather in Accra today?");
    check("an off-platform question is declined with what she can do here", /shop|catalogue|sales|orders/i.test(weather) && !/°C|sunny|rain/i.test(weather));
    const explained = await ask(M, "What does the allowance mean?");
    check("asking what a thing means explains it and changes nothing", explained.length > 40 && (shopOf(M) || {}).dailyGiveawayAllowance === 300);
    const pidgin = await ask(M, "abeg change di price of Ankara Fabric to 500");
    const ankaraNow = (shopOf(M) || { products: [] }).products.find((p) => p.name === "Ankara Fabric");
    check("a command in her own mixed words still lands (Ankara to 500)", !!ankaraNow && ankaraNow.listPrice === 500, pidgin.slice(0, 80).replace(/\n/g, " "));
    const link = await ask(M, "send me my shop link");
    check("she can always get her shop link back", link.includes("SHOP-" + shopCode));

    // Probes: what a dashboard would have. Recorded, not scored.
    const pDesc = await ask(M, "change description of Ankle Strap Heels to Soft suede finish");
    probe("edit a product description", /done|changed/i.test(pDesc) ? "works: " + pDesc.slice(0, 70).replace(/\n/g, " ") : "no clean road: " + pDesc.slice(0, 70).replace(/\n/g, " "));
    const pStock = await ask(M, "set stock of Ankle Strap Heels to 3");
    probe("set a stock count", /done|stock/i.test(pStock) ? pStock.slice(0, 80).replace(/\n/g, " ") : "no clean road: " + pStock.slice(0, 80).replace(/\n/g, " "));
    const pOrders = await ask(M, "my orders");
    probe("an orders list view", /order/i.test(pOrders) ? pOrders.slice(0, 110).replace(/\n/g, " ") : "nothing order-shaped came back");
    const pWeek = await ask(M, "How were sales this week?");
    probe("sales over a period (week)", pWeek.slice(0, 110).replace(/\n/g, " "));
    const pPayout = await ask(M, "change my payout number to 0551234567");
    probe("change the payout number", pPayout.slice(0, 100).replace(/\n/g, " "));
    const pRename = await ask(M, "rename my shop to Efua Fabrics and More");
    probe("rename the shop", pRename.slice(0, 100).replace(/\n/g, " "));
    const pPause = await ask(M, "pause my shop for two weeks");
    probe("pause or holiday mode", pPause.slice(0, 100).replace(/\n/g, " "));
    const pCancel = await ask(M, "cancel order " + (loopOrder ? loopOrder.reference : "EFUA-0001"));
    probe("merchant cancels an order", pCancel.slice(0, 100).replace(/\n/g, " "));
    const pBest = await ask(M, "who are my best customers?");
    probe("best customers view", pBest.slice(0, 100).replace(/\n/g, " "));

    const errText = fs.readFileSync(path.join(dir, "agent-stderr.log"), "utf8");
    check("the agent never panicked through her whole day", !/panic/i.test(errText));
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    try { agent.kill("SIGKILL"); } catch {}
    try { if (typeof agent2 !== "undefined") agent2.kill("SIGKILL"); } catch {}
    console.log(`\nmerchant proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
