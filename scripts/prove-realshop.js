// Live proof on a realistic shop catalogue (Monday's real
// shop test, pulled forward): one full Accra fashion shop —
// by-measure cloth, whole goods, area delivery fees, one
// sold-out line — driven end to end with the real model:
// a gift recommendation inside a stated budget, a measured
// haggle at the yard price, a basket add during fulfilment,
// delivery to an area at its own fee with the money going
// to the rider, and the merchant's own catalogue, stock and
// attention answers over the same book.
//
//   node scripts/prove-realshop.js
//
// Needs META_MODEL_API_KEY in the environment (fetched from
// the server by the operator, never written to disk).
// Checkout is dry (no Seev key in this rig): the payment
// button is the test button, which is the agreed posture
// for the hackathon — the sandbox/live key swap changes
// nothing in this flow.
"use strict";
const { spawn } = require("child_process");
const fs = require("fs");
const http = require("http");
const os = require("os");
const path = require("path");

// Sandbox hygiene, as in the sibling proofs.
delete process.env.WHATSAPP_TOKEN;
delete process.env.WHATSAPP_PHONE_NUMBER_ID;
delete process.env.WHATSAPP_APP_SECRET;
delete process.env.WHATSAPP_VERIFY_TOKEN;
delete process.env.SEEV_SECRET_KEY;
delete process.env.SEEV_API_KEY;
delete process.env.SEEV_WEBHOOK_SECRET;

if (!process.env.META_MODEL_API_KEY) {
  console.log("SKIP: META_MODEL_API_KEY missing.");
  process.exit(0);
}
const ROOT = path.join(__dirname, "..");
const PORT = 3237;
const BASE = `http://127.0.0.1:${PORT}`;
const AGENTNUM = "233209450786";
const OWNER = "233551307701";
const GIFT = "233551307702";
const BUYER = "233551307703";
const ASK = "233551307704";
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "realshop-"));
const cat = {
  merchants: {
    adom: {
      id: "adom", name: "Adom Fashion House", shopCode: "ADOM",
      ownerWhatsApp: OWNER, payoutPhone: OWNER,
      dailyGiveawayAllowance: 300,
      deliveryFee: 25, deliveryPaidTo: "carrier",
      deliveryAreas: [
        { area: "Osu", fee: 30 },
        { area: "East Legon", fee: 50 },
        { area: "Spintex", fee: 40 },
      ],
      products: [
        { id: "ankara", name: "Ankara Fabric (6 yards)", description: "Bright wax-print cotton, sold by the yard or the full 6-yard piece.", listPrice: 450, floorPrice: 360, stock: 12, unit: "yard", baseQty: 6 },
        { id: "kente", name: "Handwoven Kente Cloth (4 yards)", description: "Handwoven kente from Bonwire, sold by the yard or the full piece.", listPrice: 1800, floorPrice: 1500, stock: 3, unit: "yard", baseQty: 4 },
        { id: "smock", name: "Northern Smock (Fugu)", description: "Handwoven striped smock from the north, one size.", listPrice: 650, floorPrice: 520, stock: 5 },
        { id: "dress", name: "Woodin Print Midi Dress", description: "Woodin print midi dress with short sleeves.", listPrice: 520, floorPrice: 430, stock: 4 },
        { id: "sandals", name: "Ahenema Leather Sandals", description: "Handmade leather ahenema sandals.", listPrice: 280, floorPrice: 220, stock: 6 },
        { id: "necklace", name: "Recycled Glass Bead Necklace", description: "Krobo recycled glass beads on a cord.", listPrice: 150, floorPrice: 110, stock: 8 },
        { id: "headwrap", name: "African Print Headwrap", description: "Full-length African print headwrap.", listPrice: 85, floorPrice: 60, stock: 10 },
        { id: "shea", name: "Raw Shea Butter (500g)", description: "Unrefined shea butter from Tamale, for skin and hair.", listPrice: 120, floorPrice: 95, stock: 0 },
      ],
    },
  },
};
fs.writeFileSync(path.join(dir, "catalogue.json"), JSON.stringify(cat));

let seq = 0;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
function post(pathname, body) {
  return new Promise((resolve, reject) => {
    const req = http.request(BASE + pathname, { method: "POST", headers: { "Content-Type": "application/json" } }, (res) => {
      res.resume();
      res.on("end", resolve);
    });
    req.on("error", reject);
    req.end(JSON.stringify(body));
  });
}
function say(from, text) {
  seq++;
  const msg = { from, id: "wrs" + seq, type: "text", text: { body: text } };
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [msg] } }] }] });
}
function outboxTo(to) {
  const f = path.join(dir, "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)).filter((r) => r.to === to);
}
const cursors = {};
async function fresh(to, timeoutMs) {
  const end = Date.now() + (timeoutMs || 120000);
  cursors[to] = cursors[to] || 0;
  while (Date.now() < end) {
    const all = outboxTo(to);
    if (all.length > cursors[to]) {
      const got = all.slice(cursors[to]);
      cursors[to] = all.length;
      await sleep(4000);
      const more = outboxTo(to);
      if (more.length > cursors[to]) { got.push(...more.slice(cursors[to])); cursors[to] = more.length; }
      return got;
    }
    await sleep(1500);
  }
  return [];
}
const text = (rows) => rows.map((r) => r.body || "").join("\n");
function orders() {
  const f = path.join(dir, "orders.json");
  if (!fs.existsSync(f)) return {};
  return JSON.parse(fs.readFileSync(f, "utf8"));
}
function catalogue() {
  return JSON.parse(fs.readFileSync(path.join(dir, "catalogue.json"), "utf8"));
}
async function waitHealth() {
  for (let i = 0; i < 40; i++) {
    try {
      await new Promise((resolve, reject) => {
        http.get(BASE + "/healthz", (res) => { res.resume(); res.on("end", resolve); }).on("error", reject);
      });
      return;
    } catch (e) { await sleep(1000); }
  }
  throw new Error("agent did not come up");
}

(async () => {
  let failures = 0;
  const check = (ok, label, evidence) => {
    console.log((ok ? "PASS: " : "FAIL: ") + label + (ok ? "" : " — " + (evidence || "").slice(0, 500)));
    if (!ok) failures++;
  };
  const child = spawn(path.join(ROOT, "agent"), [], {
    cwd: ROOT,
    env: { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: dir, WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1", MERCHANT_WHATSAPP: OWNER, AGENT_WHATSAPP_NUMBER: AGENTNUM },
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.stderr.on("data", (d) => process.stderr.write("[agent] " + d));
  try {
    await waitHealth();

    // Merchant leg A: her own catalogue, from the book.
    await say(OWNER, "Show me my catalogue");
    let got = text(await fresh(OWNER));
    check(/Ankara Fabric/.test(got) && /GH₵450/.test(got) && /Kente/.test(got),
      "merchant catalogue lists the real shelf at its prices", got);

    // Buyer leg 1: a gift inside a stated budget, unbound.
    await say(GIFT, "Hello, I need a gift for my mother. My budget is 600 cedis");
    got = text(await fresh(GIFT));
    check(/Woodin Print Midi Dress|Northern Smock|Ahenema|Glass Bead|Headwrap/i.test(got),
      "gift ask inside 600 is met from the real shelf", got);

    // Buyer leg 2: the measured purchase.
    await say(BUYER, "SHOP-ADOM");
    await fresh(BUYER);
    await say(BUYER, "I need 3 yards of the ankara fabric");
    got = text(await fresh(BUYER));
    check(/225/.test(got), "3 yards of a 450 six-yard piece quotes 225", got);
    await say(BUYER, "Can you do the 3 yards for 180?");
    got = text(await fresh(BUYER));
    check(!/yours for GH₵(1[0-7][0-9])/.test(got), "no agreement below the 180 floor for 3 yards", got);
    // Settle the haggle adaptively: track the agreed figure
    // from the agent's own words, accepting its counter if
    // it makes one.
    let agreed = 0;
    const dealFigure = (s) => {
      const m = s.match(/yours for GH₵([\d,]+)/);
      return m ? parseFloat(m[1].replace(/,/g, "")) : 0;
    };
    agreed = dealFigure(got);
    if (!agreed) {
      await say(BUYER, "Okay, 200 then");
      got = text(await fresh(BUYER));
      agreed = dealFigure(got);
      if (!agreed) {
        const figs = [...got.matchAll(/GH₵([\d,]+)/g)].map((m) => parseFloat(m[1].replace(/,/g, ""))).filter((n) => n >= 180 && n <= 225);
        if (figs.length) {
          await say(BUYER, "I will take it at " + figs[figs.length - 1]);
          got = text(await fresh(BUYER));
          agreed = dealFigure(got) || figs[figs.length - 1];
        }
      }
    }
    check(agreed >= 180 && agreed <= 225, "the haggle settles between the 180 floor and the 225 ask", got + " agreed=" + agreed);

    // Fulfilment: delivery to East Legon, a basket add on
    // the way, answered by the prompt actually received.
    let added = false;
    let last = got;
    for (let turn = 0; turn < 18 && Object.keys(orders()).length === 0; turn++) {
      const prompt = last;
      console.log("buyer prompt: " + prompt.replace(/\n/g, " | ").slice(0, 220));
      let reply = null;
      // The added item is put in front of her for its own
      // agreement before the walk resumes (the basket's
      // design): an offer at its list price closes it at
      // once, and fulfilment then reopens for the whole
      // order.
      if (/Make me an offer/i.test(prompt)) reply = "I will take it at 85";
      else if (/Add the African Print Headwrap/i.test(prompt)) reply = "yes";
      else if (/confirm your order|tell me what to change/i.test(prompt)) reply = "yes";
      else if (/where|which area|what area|location/i.test(prompt)) reply = "East Legon";
      else if (/phone number|your number/i.test(prompt)) reply = "0551234567";
      else if (/name.*package|package.*name|name should go/i.test(prompt)) reply = "Ama Serwaa";
      else if (/message for the seller|any note/i.test(prompt)) reply = "no";
      else if (/deliver|pick it up|collect|meet/i.test(prompt)) reply = "Please deliver it to me";
      if (reply === null) { console.log("driver: no reply matched, stopping"); break; }
      console.log("buyer replies: " + reply);
      await say(BUYER, reply);
      last = text(await fresh(BUYER));
      if (!added && Object.keys(orders()).length === 0) {
        // Mid-fulfilment, she remembers the headwrap.
        await say(BUYER, "Please add the headwrap too");
        const addReply = text(await fresh(BUYER));
        console.log("add attempt reply: " + addReply.replace(/\n/g, " | ").slice(0, 220));
        if (/Add the African Print Headwrap at GH₵85/.test(addReply)) {
          added = true;
          await say(BUYER, "Yes, add it");
          last = text(await fresh(BUYER));
        }
      }
    }
    check(added, "the headwrap is offered as a basket add during fulfilment");
    const book = Object.values(orders());
    check(book.length === 1, "exactly one order was made", JSON.stringify(book).slice(0, 300));
    if (book.length === 1) {
      const o = book[0];
      check(o.total === agreed + 85, "the charged total is goods only: the agreed cut + 85 headwrap", JSON.stringify(o).slice(0, 300) + " agreed=" + agreed);
      check(o.deliveryFee === 50 && o.deliveryPaidTo === "carrier",
        "East Legon's own fee (50) is recorded as the rider's money, not the shop's", JSON.stringify(o).slice(0, 300));
      check(/Ankara/.test(o.productName || "") || (o.items || []).length > 0,
        "the order carries the measured line", JSON.stringify(o).slice(0, 300));
    }
    const ank = catalogue().merchants.adom.products.find((p) => p.id === "ankara");
    const unitsLeft = ank.stockUnits != null ? ank.stockUnits : ank.stock * 6;
    check(unitsLeft === 69, "the cloth book drops by exactly 3 yards (72 -> 69)", JSON.stringify(ank));

    // Buyer leg 3: the sold-out line, asked for by name.
    await say(ASK, "SHOP-ADOM");
    await fresh(ASK);
    await say(ASK, "Do you have the shea butter?");
    got = text(await fresh(ASK));
    check(/sold out/i.test(got) && !/Still available/i.test(got),
      "the sold-out shea butter is answered honestly on a real shelf", got);

    // Merchant leg B: the day's attention over the same book.
    const ref = book.length === 1 ? book[0].reference : "";
    await say(OWNER, "What needs my attention today?");
    got = text(await fresh(OWNER));
    check(ref !== "" && got.includes(ref), "the attention list carries the waiting order by reference", got);

    // Merchant leg C: her stock, in yards.
    await say(OWNER, "How much ankara is left?");
    got = text(await fresh(OWNER));
    check(/69|11/.test(got), "the merchant's stock answer reads the same book (69 yards / 11 pieces)", got);
  } finally {
    child.kill("SIGTERM");
  }
  console.log(failures === 0 ? "RESULT: PASS" : `RESULT: FAIL (${failures})`);
  process.exit(failures === 0 ? 0 : 1);
})();
