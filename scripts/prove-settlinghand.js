// Live proof for the settling hand fix, on the real
// model key, against a real spawned process — the
// owner's own phone test replayed exactly: Thong
// Sandals GH₵80 and a Double Clothes Rack GH₵270, a
// GH₵350 hand, settled one by one after the Done tap.
//   - leg 1: she picks both, taps Done, and offers "I
//     will give you 330 for all" at the first card. The
//     offer opens the haggle on the whole hand: no item
//     closes at its own price, the total is named at
//     350, and the talk is over the one figure.
//   - leg 2: the talk agrees one total inside the
//     hand's bounds and the hand completes as ONE order
//     whose items sit at or above their own last prices
//     and sum to the agreed total exactly.
//   - leg 3: a second buyer settles the sandals at their
//     price first, then offers 330 for all at the rack's
//     card. The agent asks the one scope question and
//     binds nothing. She answers "everything", the 330
//     is read as covering the settled sandals too, and
//     the remaining 250 is heard on the rack.
//   - leg 4: a third buyer offers 100 for the 80 sandals
//     alone, and the agent corrects her aloud — the price
//     is 80, she cannot offer above it — and the deal
//     closes at GH₵80: an offer above the price never
//     charges above the price.
// Run:
//   META_MODEL_API_KEY=<redacted> node scripts/prove-settlinghand.js
"use strict";
const { spawn } = require("child_process");
const fs = require("fs");
const http = require("http");
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

if (!process.env.META_MODEL_API_KEY) {
  console.log("SKIP: META_MODEL_API_KEY missing.");
  process.exit(0);
}
const ROOT = path.join(__dirname, "..");
const PORT = 3241;
const BASE = `http://127.0.0.1:${PORT}`;
const OWNER = "233551209601";
const BUYER_A = "233551209501";
const BUYER_B = "233551209502";
const BUYER_C = "233551209503";
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "settlinghand-"));
const cat = { merchants: { luxe: { id: "luxe", name: "Luxe", shopCode: "LUXE", ownerWhatsApp: OWNER, payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 25, deliveryNote: "Sent by bus to your station", products: [
  { id: "sandals", name: "Thong Sandals", description: "Thong sandals, sizes 37-41.", listPrice: 80, floorPrice: 60, stock: 5 },
  { id: "rack", name: "Double Clothes Rack", description: "Black metal double clothes rack with bottom storage shelf.", listPrice: 270, floorPrice: 250, stock: 4 },
] } } };
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
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from, id: "wsh" + seq, type: "text", text: { body: text } }] } }] }] });
}
function tap(from, rowID, title) {
  seq++;
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from, id: "wsh" + seq, type: "interactive", interactive: { type: "list_reply", list_reply: { id: rowID, title: title || "" } } }] } }] }] });
}
function outbox(to) {
  const f = path.join(dir, "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)).filter((r) => r.to === to);
}
const cursors = {};
async function fresh(to, timeoutMs) {
  const end = Date.now() + (timeoutMs || 90000);
  let cursor = cursors[to] || 0;
  while (Date.now() < end) {
    const all = outbox(to);
    if (all.length > cursor) {
      const got = all.slice(cursor);
      cursor = all.length;
      await sleep(4000);
      const more = outbox(to);
      if (more.length > cursor) { got.push(...more.slice(cursor)); cursor = more.length; }
      cursors[to] = cursor;
      return got;
    }
    await sleep(1500);
  }
  cursors[to] = cursor;
  return [];
}
const texts = (recs) => recs.map((r) => r.body || "").join("\n");
const figures = (txt) => [...txt.replace(/,/g, "").matchAll(/GH₵([\d.]+)/g)].map((m) => parseFloat(m[1]));
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
async function pickBothAndDone(buyer) {
  await say(buyer, "SHOP-LUXE");
  await fresh(buyer);
  await tap(buyer, "shelf_sandals", "Thong Sandals");
  await fresh(buyer, 60000);
  await tap(buyer, "shelf_rack", "Double Clothes Rack");
  await fresh(buyer, 60000);
  await tap(buyer, "shelfdone", "Done, settle them");
  return fresh(buyer);
}
async function finishOrder(buyer) {
  await say(buyer, "Send it to Accra");
  let recs = await fresh(buyer);
  if (!texts(recs).includes("What name should go on the package?")) return recs;
  await say(buyer, "Efua Mensima");
  await fresh(buyer);
  await say(buyer, "no");
  await fresh(buyer);
  await say(buyer, "yes");
  await fresh(buyer);
  return recs;
}
function readOrders() {
  const f = path.join(dir, "orders.json");
  if (!fs.existsSync(f)) return [];
  return Object.values(JSON.parse(fs.readFileSync(f, "utf8")));
}

(async () => {
  let failures = 0;
  const check = (ok, label, evidence) => {
    console.log((ok ? "PASS: " : "FAIL: ") + label + (ok ? "" : " — " + (evidence || "").slice(0, 300)));
    if (!ok) failures++;
  };
  const child = spawn(path.join(ROOT, "agent"), [], {
    cwd: ROOT,
    env: { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: dir, WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1" },
    stdio: "ignore",
  });
  try {
    await waitHealth();

    // Legs 1-2: the screenshot itself.
    let recs = await pickBothAndDone(BUYER_A);
    check(texts(recs).includes("one by one") && texts(recs).includes("Thong Sandals"),
      "leg 1 setup: the settling opens on the sandals", texts(recs));
    await say(BUYER_A, "I will give you 330 for all");
    recs = await fresh(BUYER_A);
    check(!texts(recs).includes("Thong Sandals is yours for GH₵80"),
      "leg 1a: the hand offer does not close the sandals at their own price", texts(recs));
    check(texts(recs).includes("For all of them together") && texts(recs).includes("GH₵350"),
      "leg 1b: the offer opens the haggle on the whole 350 hand", texts(recs));

    if (!texts(recs).includes("yours for")) {
      const figs = figures(texts(recs)).filter((f) => f >= 310 && f < 350);
      const counter = figs[figs.length - 1];
      check(counter !== undefined, "leg 2 setup: the counter names a figure inside the hand's bounds", texts(recs));
      if (counter !== undefined) {
        await say(BUYER_A, String(counter));
        recs = await fresh(BUYER_A);
      }
    }
    const mA = texts(recs).match(/That makes your order GH₵([\d,]+(?:\.\d+)?) in all/);
    const totalA = mA ? parseFloat(mA[1].replace(/,/g, "")) : NaN;
    check(texts(recs).includes("yours for") && totalA >= 310 && totalA <= 350,
      "leg 2a: the talk agrees one total between the summed last prices (310) and the hand (350)", texts(recs));
    await finishOrder(BUYER_A);
    let all = readOrders().filter((o) => o.from === BUYER_A);
    let orderA = all[0];
    check(!!orderA && orderA.items && orderA.items.length === 2 && orderA.price === totalA &&
      orderA.items.reduce((a, it) => a + it.price, 0) === orderA.price &&
      orderA.items.every((it) => it.price >= { "Thong Sandals": 60, "Double Clothes Rack": 250 }[it.name]),
      "leg 2b: one order, two items, none below its last price, summing to the agreed total",
      JSON.stringify(orderA || all).slice(0, 300));

    // Leg 3: one item settled first, then the scope question.
    recs = await pickBothAndDone(BUYER_B);
    await say(BUYER_B, "80");
    recs = await fresh(BUYER_B);
    check(texts(recs).includes("Thong Sandals is yours for GH₵80") && texts(recs).includes("Double Clothes Rack"),
      "leg 3 setup: the sandals settle at their price and the rack comes up", texts(recs));
    await say(BUYER_B, "I will give you 330 for all");
    recs = await fresh(BUYER_B);
    check(!texts(recs).includes("Double Clothes Rack is yours for"),
      "leg 3a: the hand offer binds nothing before its scope is declared", texts(recs));
    check(texts(recs).includes("already settled") && texts(recs).includes("for everything"),
      "leg 3b: the agent asks whether the figure covers everything or only the rest", texts(recs));
    await say(BUYER_B, "everything");
    recs = await fresh(BUYER_B);
    if (!texts(recs).includes("yours for")) {
      const figs = figures(texts(recs)).filter((f) => f >= 250 && f < 270);
      const counter = figs[figs.length - 1];
      if (counter !== undefined) {
        await say(BUYER_B, String(counter));
        recs = await fresh(BUYER_B);
      }
    }
    check(texts(recs).includes("Double Clothes Rack is yours for"),
      "leg 3c: on 'everything' the remainder is heard on the rack and it settles", texts(recs));
    await finishOrder(BUYER_B);
    all = readOrders().filter((o) => o.from === BUYER_B);
    const orderB = all[0];
    check(!!orderB && orderB.items && orderB.items.length === 2 &&
      orderB.items.find((it) => it.name === "Thong Sandals").price === 80 &&
      orderB.price >= 330 && orderB.price <= 350,
      "leg 3d: one order, the settled sandals standing at 80, the total inside her 330-to-350 span",
      JSON.stringify(orderB || all).slice(0, 300));

    // Leg 4: an over-offer on one item closes at the price.
    await say(BUYER_C, "SHOP-LUXE");
    await fresh(BUYER_C);
    await say(BUYER_C, "I will give you 100 for the Thong Sandals");
    recs = await fresh(BUYER_C);
    check(texts(recs).includes("is GH₵80, not GH₵100") && texts(recs).includes("cannot offer above the price") &&
      texts(recs).includes("Thong Sandals is yours for GH₵80") && !texts(recs).includes("yours for GH₵100"),
      "leg 4: an offer of 100 for the 80 sandals is corrected aloud and closes at GH₵80, never above the price", texts(recs));
  } finally {
    child.kill("SIGTERM");
  }
  console.log(failures === 0 ? "PROOF GREEN" : "PROOF RED: " + failures + " leg(s) failed");
  process.exit(failures === 0 ? 0 : 1);
})().catch((e) => { console.log("FAIL: " + e.message); process.exit(1); });
