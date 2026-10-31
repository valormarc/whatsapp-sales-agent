// Live proof replaying the owner's own counter journey,
// the one that produced six defects in four minutes:
// he asked for his basket in his own words and got
// prose, offered for the hand and haggled, said "Deal" at
// the standing counter and was stalled twice, answered
// "Same" twice and had the word Same stored as his note,
// and the seller was asked to confirm one item at the
// whole order's price. Every leg below is one of those
// moments, on a synthetic shop, with the real model key.
// Run:
//   META_MODEL_API_KEY=<key> node scripts/prove-counterlife.js
"use strict";
const { spawn } = require("child_process");
const fs = require("fs");
const http = require("http");
const os = require("os");
const path = require("path");

// Sandbox hygiene: a dry-run child must never inherit live
// send or settle credentials from the operator's shell.
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
const PORT = 3247;
const BASE = `http://127.0.0.1:${PORT}`;
const OWNER = "233551209801";
const BUYER = "233551209811";
const BUYER_B = "233551209812";
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "counterlife-"));
const cat = { merchants: { luxe: { id: "luxe", name: "Luxe", shopCode: "LUXE", ownerWhatsApp: OWNER, payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 25, deliveryNote: "Sent by bus to your station", products: [
  { id: "slippers", name: "Studded Thong Slippers", description: "Studded thong slippers.", listPrice: 80, floorPrice: 70, stock: 1 },
  { id: "heels", name: "Ankle Strap Heels", description: "Ankle strap heels.", listPrice: 130, floorPrice: 110, stock: 1 },
  { id: "rack", name: "Double Clothes Rack", description: "Black metal double clothes rack.", listPrice: 270, floorPrice: 250, stock: 4 },
] } } };
fs.writeFileSync(path.join(dir, "catalogue.json"), JSON.stringify(cat));
// Her last order at this shop: the name and the note her
// "Same" answers must carry.
const prev = { reference: "LUXE-0001", status: "paid", from: BUYER, merchantId: "luxe", productName: "Thong Sandals", price: 80, packageName: "Tsey Addo", note: "Pink and white", createdAt: "2026-10-30T10:00:00Z" };
fs.writeFileSync(path.join(dir, "orders.json"), JSON.stringify({ "LUXE-0001": prev }));

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
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from, id: "wcl" + seq, type: "text", text: { body: text } }] } }] }] });
}
function tap(from, rowID, title) {
  seq++;
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from, id: "wcl" + seq, type: "interactive", interactive: { type: "list_reply", list_reply: { id: rowID, title: title || "" } } }] } }] }] });
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
    console.log((ok ? "PASS: " : "FAIL: ") + label + (ok ? "" : " — " + (evidence || "").slice(0, 500)));
    if (!ok) failures++;
  };
  const child = spawn(path.join(ROOT, "agent"), [], {
    cwd: ROOT,
    env: { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: dir, WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1" },
    stdio: "ignore",
  });
  try {
    await waitHealth();

    // Setup: bind, pick both one-off items, Done.
    await say(BUYER, "SHOP-LUXE");
    await fresh(BUYER);
    await tap(BUYER, "shelf_slippers", "Studded Thong Slippers");
    await fresh(BUYER, 60000);
    await tap(BUYER, "shelf_heels", "Ankle Strap Heels");
    await fresh(BUYER, 60000);
    await tap(BUYER, "shelfdone", "Done, settle them");
    let recs = await fresh(BUYER);
    check(texts(recs).includes("Studded Thong Slippers"), "setup: the settling opens on the slippers", texts(recs));

    // Leg 1: his own words for the basket ask.
    await say(BUYER, "Check basket");
    recs = await fresh(BUYER);
    console.log("LEG 1 REPLY:\n" + texts(recs));
    check(texts(recs).includes("Studded Thong Slippers - GH₵80") && texts(recs).includes("Ankle Strap Heels - GH₵130"),
      "leg 1: 'Check basket' is answered with the basket, listed at shelf prices", texts(recs));
    check(!texts(recs).includes("such as"), "leg 1: no prose deflection strings the goods into a sentence", texts(recs));

    // Leg 2: an offer for the whole hand, below the floors,
    // so a counter must stand.
    await say(BUYER, "150 for all");
    recs = await fresh(BUYER);
    console.log("LEG 2 REPLY:\n" + texts(recs));
    check(texts(recs).includes("GH₵210 in all"), "leg 2: the hand offer is heard on the hand, at its shelf total", texts(recs));
    const counterFigs = figures(texts(recs)).filter((f) => f > 150 && f < 210);
    const counter = counterFigs[counterFigs.length - 1];
    check(counter !== undefined, "leg 2: a counter stands on the hand", texts(recs));

    // Leg 3: THE defect — "Deal" at the standing counter.
    await say(BUYER, "Deal 🤝");
    recs = await fresh(BUYER);
    console.log("LEG 3 REPLY:\n" + texts(recs));
    check(texts(recs).includes("Shall I send the payment link?"),
      "leg 3: 'Deal' at the standing counter opens the confirmation, not a stall", texts(recs));
    check(texts(recs).includes("All of them together are GH₵" + (counter || "")),
      "leg 3: the confirmation names the hand as all of them together, at the counter", texts(recs));

    // Leg 4: confirm, then walk the fulfilment: delivery,
    // a real place, and "Same" twice.
    await say(BUYER, "yes");
    recs = await fresh(BUYER);
    check(texts(recs).includes("How would you like to get it?") || texts(recs).includes("How do you want to get it?"),
      "leg 4 setup: the fulfilment choice arrives", texts(recs));
    await say(BUYER, "delivery");
    recs = await fresh(BUYER);
    check(texts(recs).includes("Where should it go?"), "leg 4 setup: the place question arrives", texts(recs));
    await say(BUYER, "Accra");
    recs = await fresh(BUYER);
    check(texts(recs).includes("What name should go on the package?"), "leg 4 setup: the name question arrives", texts(recs));
    await say(BUYER, "Same");
    recs = await fresh(BUYER);
    check(texts(recs).includes("Any message for the seller"), "leg 4 setup: the note question arrives after 'Same' at the name step", texts(recs));
    await say(BUYER, "Same");
    recs = await fresh(BUYER);
    console.log("LEG 4 RECAP:\n" + texts(recs));
    check(texts(recs).includes("Your message for the seller: Pink and white."),
      "leg 4: 'Same' at the note step carries the last note, and the recap prints it", texts(recs));
    check(!texts(recs).includes("seller: Same") && !texts(recs).includes("is Same"),
      "leg 4: the word Same itself is never stored as the note", texts(recs));
    check(texts(recs).includes("Accra"), "leg 4: the recap carries the place she gave", texts(recs));
    const shareMatch = texts(recs).match(/Ankle Strap Heels - GH₵([\d.]+)/);
    check(!!shareMatch, "leg 4: the recap lists the heels at their share", texts(recs));
    const heelsShare = shareMatch ? shareMatch[1] : "";

    // Leg 5: confirm the order; the in-hand gate must ask
    // the seller about the gated item at ITS share.
    await say(BUYER, "yes");
    recs = await fresh(BUYER);
    console.log("LEG 5 BUYER SIDE:\n" + texts(recs));
    check(/only one the shop has/.test(texts(recs)) && texts(recs).includes("Ankle Strap Heels"),
      "leg 5: the buyer's wait names the gated item", texts(recs));
    const ownerRecs = await fresh(OWNER, 60000);
    console.log("LEG 5 SELLER ASK:\n" + texts(ownerRecs));
    check(texts(ownerRecs).includes("Ankle Strap Heels (GH₵" + heelsShare + ")"),
      "leg 5: the seller is asked about the heels at their own share", texts(ownerRecs));
    check(!texts(ownerRecs).includes("(GH₵" + (counter || "never") + ")"),
      "leg 5: the seller is never asked at the whole order's total for one item", texts(ownerRecs));

    // Leg 6 (second buyer): a greeting is not a place. At
    // the delivery place question she says "Hello"; the
    // walk must ask again, not settle on it.
    await say(BUYER_B, "SHOP-LUXE");
    await fresh(BUYER_B);
    await tap(BUYER_B, "shelf_rack", "Double Clothes Rack");
    await fresh(BUYER_B, 60000);
    await tap(BUYER_B, "shelfdone", "Done, settle them");
    await fresh(BUYER_B);
    await say(BUYER_B, "270");
    recs = await fresh(BUYER_B);
    check(texts(recs).includes("How would you like to get it?") || texts(recs).includes("How do you want to get it?"),
      "leg 6 setup: the rack settles at its price and the walk opens", texts(recs));
    await say(BUYER_B, "delivery");
    recs = await fresh(BUYER_B);
    check(texts(recs).includes("Where should it go?"), "leg 6 setup: the place question arrives", texts(recs));
    await say(BUYER_B, "Hello");
    recs = await fresh(BUYER_B);
    console.log("LEG 6 REPLY:\n" + texts(recs));
    check(texts(recs).includes("Where should it go?"),
      "leg 6: a greeting at the place question is asked again, never settled as her place", texts(recs));
    await say(BUYER_B, "Accra");
    recs = await fresh(BUYER_B);
    check(texts(recs).includes("What name should go on the package?"),
      "leg 6: a real place moves the walk on", texts(recs));
  } finally {
    child.kill();
    fs.rmSync(dir, { recursive: true, force: true });
  }
  console.log(failures === 0 ? "PROOF GREEN" : `PROOF RED: ${failures} leg(s) failed`);
  process.exit(failures === 0 ? 0 : 1);
})();
