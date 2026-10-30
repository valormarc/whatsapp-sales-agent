// Live proof for the typed road around the picks, on
// the real model key, against a real spawned process —
// the owner's own phone journey, replayed: rack 270,
// ribbed dress 130, halter dress 150, a 550 hand.
//   - leg 1: a typed "Done" after two taps starts the
//     settling exactly as the Done row does;
//   - leg 2: "I will give you 509" is taken on the whole
//     hand: the total is named at 550 and the talk is
//     over the one figure;
//   - leg 3: meeting the answer agrees a total between
//     the summed last prices (500) and the 550 list, and
//     the deal names that order total;
//   - leg 4: the journey completes as ONE order whose
//     three items each sit at or above their own last
//     price and sum to the agreed total exactly;
//   - leg 5: "I will give you 250 for all" — a declared
//     total BELOW the dearest pick (the 270 rack) — is
//     still read on the whole hand, because she said
//     what the figure was for.
// Run:
//   META_MODEL_API_KEY=<redacted> node scripts/prove-total.js
"use strict";
const { spawn } = require("child_process");
const fs = require("fs");
const http = require("http");
const os = require("os");
const path = require("path");

if (!process.env.META_MODEL_API_KEY) {
  console.log("SKIP: META_MODEL_API_KEY missing.");
  process.exit(0);
}
const ROOT = path.join(__dirname, "..");
const PORT = 3227;
const BASE = `http://127.0.0.1:${PORT}`;
const OWNER = "233551209301";
const BUYER_A = "233551209401";
const BUYER_B = "233551209402";
const BUYER_C = "233551209403";
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "total-"));
const cat = { merchants: { hand: { id: "hand", name: "Hand Shop", shopCode: "HAND", ownerWhatsApp: OWNER, payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 20, deliveryNote: "Sent by bus to your station", products: [
  { id: "rack", name: "Double Clothes Rack", description: "Black metal double clothes rack.", listPrice: 270, floorPrice: 250, stock: 2 },
  { id: "ribbed", name: "Ribbed Off-Shoulder Bodycon Maxi Dress", description: "Ribbed off-shoulder dress.", listPrice: 130, floorPrice: 120, stock: 2 },
  { id: "halter", name: "Halter Pleated Maxi Dress", description: "Green halter-neck pleated maxi dress.", listPrice: 150, floorPrice: 130, stock: 2 },
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
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from, id: "wt" + seq, type: "text", text: { body: text } }] } }] }] });
}
function tap(from, rowID, title) {
  seq++;
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from, id: "wt" + seq, type: "interactive", interactive: { type: "list_reply", list_reply: { id: rowID, title: title || "" } } }] } }] }] });
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

(async () => {
  let failures = 0;
  const check = (ok, label, evidence) => {
    console.log((ok ? "PASS: " : "FAIL: ") + label + (ok ? "" : " — " + (evidence || "").slice(0, 300)));
    if (!ok) failures++;
  };
  const child = spawn(path.join(ROOT, "agent"), [], {
    cwd: ROOT,
    env: { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: dir, WHATSAPP_DRY_RUN: "1" },
    stdio: "ignore",
  });
  try {
    await waitHealth();

    // Leg 1: typed Done.
    await say(BUYER_A, "SHOP-HAND");
    await fresh(BUYER_A);
    await tap(BUYER_A, "shelf_rack", "Double Clothes Rack");
    await fresh(BUYER_A, 60000);
    await tap(BUYER_A, "shelf_ribbed", "Ribbed Off-Shoulder");
    await fresh(BUYER_A, 60000);
    await say(BUYER_A, "Done");
    let recs = await fresh(BUYER_A);
    check(texts(recs).includes("one by one") && texts(recs).includes("Double Clothes Rack") && texts(recs).includes("GH₵270"),
      "leg 1: a typed Done starts the settling on the first pick", texts(recs));

    // Legs 2-4: the total offer.
    await say(BUYER_B, "SHOP-HAND");
    await fresh(BUYER_B);
    await tap(BUYER_B, "shelf_rack", "Double Clothes Rack");
    await fresh(BUYER_B, 60000);
    await tap(BUYER_B, "shelf_ribbed", "Ribbed Off-Shoulder");
    await fresh(BUYER_B, 60000);
    await tap(BUYER_B, "shelf_halter", "Halter Pleated Maxi");
    await fresh(BUYER_B, 60000);

    await say(BUYER_B, "I will give you 509");
    recs = await fresh(BUYER_B);
    check(texts(recs).includes("For all of them together") && texts(recs).includes("GH₵550"),
      "leg 2: the offer is taken on the whole hand at its 550 total", texts(recs));

    if (!texts(recs).includes("yours for")) {
      const figs = figures(texts(recs)).filter((f) => f >= 500 && f < 550);
      const counter = figs[figs.length - 1];
      check(counter !== undefined, "leg 3 setup: the counter names a figure inside the hand's bounds", texts(recs));
      if (counter !== undefined) {
        await say(BUYER_B, String(counter));
        recs = await fresh(BUYER_B);
      }
    }
    const m = texts(recs).match(/That makes your order GH₵([\d,]+(?:\.\d+)?) in all/);
    const agreedTotal = m ? parseFloat(m[1].replace(/,/g, "")) : NaN;
    check(texts(recs).includes("yours for") && agreedTotal >= 500 && agreedTotal <= 550,
      "leg 3: the talk agrees one total between the summed last prices and the list", texts(recs));

    await say(BUYER_B, "Send it to Koforidua");
    recs = await fresh(BUYER_B);
    check(texts(recs).includes("What name should go on the package?"), "leg 4 setup: one package name for the whole order", texts(recs));
    await say(BUYER_B, "Efua Mensima");
    await fresh(BUYER_B);
    await say(BUYER_B, "no");
    await fresh(BUYER_B);
    await say(BUYER_B, "yes");
    await fresh(BUYER_B);
    const ordersFile = path.join(dir, "orders.json");
    let orderOK = false, evidence = "no orders file";
    if (fs.existsSync(ordersFile)) {
      const all = Object.values(JSON.parse(fs.readFileSync(ordersFile, "utf8")));
      evidence = JSON.stringify(all).slice(0, 400);
      if (all.length === 1 && all[0].items && all[0].items.length === 3) {
        const floors = { "Double Clothes Rack": 250, "Ribbed Off-Shoulder Bodycon Maxi Dress": 120, "Halter Pleated Maxi Dress": 130 };
        const sum = all[0].items.reduce((a, it) => a + it.price, 0);
        orderOK = all[0].price === agreedTotal && sum === all[0].price && all[0].items.every((it) => it.price >= floors[it.name]);
      }
    }
    check(orderOK, "leg 4: one order, three items, none below its last price, summing to the agreed total", evidence);

    // Leg 5: the declared total below the dearest pick.
    await say(BUYER_C, "SHOP-HAND");
    await fresh(BUYER_C);
    await tap(BUYER_C, "shelf_rack", "Double Clothes Rack");
    await fresh(BUYER_C, 60000);
    await tap(BUYER_C, "shelf_ribbed", "Ribbed Off-Shoulder");
    await fresh(BUYER_C, 60000);
    await tap(BUYER_C, "shelf_halter", "Halter Pleated Maxi");
    await fresh(BUYER_C, 60000);
    await say(BUYER_C, "I will give you 250 for all");
    recs = await fresh(BUYER_C);
    check(texts(recs).includes("For all of them together") && texts(recs).includes("GH₵550"),
      "leg 5: '250 for all' is read on the whole hand even though it is below the dearest pick", texts(recs));
  } finally {
    child.kill("SIGTERM");
  }
  console.log(failures === 0 ? "PROOF GREEN" : "PROOF RED: " + failures + " leg(s) failed");
  process.exit(failures === 0 ? 0 : 1);
})().catch((e) => { console.log("FAIL: " + e.message); process.exit(1); });
