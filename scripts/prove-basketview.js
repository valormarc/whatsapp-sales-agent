// Live proof for the basket view: a buyer who asks to see
// her basket gets it listed, one item per line at its
// price, never strung into a sentence — at every stage of
// the order: picks not yet settled, one item settled with
// the next in front of her, and a whole hand agreed at one
// total. Written after the owner's own phone test: he asked
// to see his basket and the items came back in a sentence,
// because no basket view existed and the ask fell to the
// model's drafted talk. Run:
//   META_MODEL_API_KEY=<key> node scripts/prove-basketview.js
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
const PORT = 3243;
const BASE = `http://127.0.0.1:${PORT}`;
const OWNER = "233551209701";
const BUYER_A = "233551209711";
const BUYER_B = "233551209712";
const BUYER_C = "233551209713";
const BUYER_D = "233551209714";
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "basketview-"));
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
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from, id: "wbv" + seq, type: "text", text: { body: text } }] } }] }] });
}
function tap(from, rowID, title) {
  seq++;
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from, id: "wbv" + seq, type: "interactive", interactive: { type: "list_reply", list_reply: { id: rowID, title: title || "" } } }] } }] }] });
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
async function pickBoth(buyer) {
  await say(buyer, "SHOP-LUXE");
  await fresh(buyer);
  await tap(buyer, "shelf_sandals", "Thong Sandals");
  await fresh(buyer, 60000);
  await tap(buyer, "shelf_rack", "Double Clothes Rack");
  return fresh(buyer, 60000);
}

(async () => {
  let failures = 0;
  const check = (ok, label, evidence) => {
    console.log((ok ? "PASS: " : "FAIL: ") + label + (ok ? "" : " — " + (evidence || "").slice(0, 400)));
    if (!ok) failures++;
  };
  const child = spawn(path.join(ROOT, "agent"), [], {
    cwd: ROOT,
    env: { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: dir, WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1" },
    stdio: "ignore",
  });
  try {
    await waitHealth();

    // Leg A: one item settled, the next in front of her.
    let recs = await pickBoth(BUYER_A);
    await tap(BUYER_A, "shelfdone", "Done, settle them");
    recs = await fresh(BUYER_A);
    check(texts(recs).includes("one by one") && texts(recs).includes("Thong Sandals"),
      "leg A setup: the settling opens on the sandals", texts(recs));
    await say(BUYER_A, "80");
    recs = await fresh(BUYER_A);
    check(texts(recs).includes("Thong Sandals is yours for GH₵80"),
      "leg A setup: the sandals settle at their price", texts(recs));
    await say(BUYER_A, "show me what's in my basket");
    recs = await fresh(BUYER_A);
    console.log("LEG A BASKET REPLY:\n" + texts(recs));
    check(texts(recs).includes("Thong Sandals - GH₵80"),
      "leg A: the settled sandals are listed at their agreed price", texts(recs));
    check(texts(recs).includes("Double Clothes Rack - GH₵270"),
      "leg A: the rack in front of her is listed at its shelf price", texts(recs));

    // Leg B: picks only, nothing settled yet.
    recs = await pickBoth(BUYER_B);
    await say(BUYER_B, "show me my basket");
    recs = await fresh(BUYER_B);
    console.log("LEG B BASKET REPLY:\n" + texts(recs));
    check(texts(recs).includes("Thong Sandals - GH₵80") && texts(recs).includes("Double Clothes Rack - GH₵270"),
      "leg B: the picks are listed one per line at their shelf prices", texts(recs));

    // Leg C: a whole hand agreed at one total — the
    // owner's own moment when he asked.
    recs = await pickBoth(BUYER_C);
    await tap(BUYER_C, "shelfdone", "Done, settle them");
    recs = await fresh(BUYER_C);
    await say(BUYER_C, "I will give you 330 for all");
    recs = await fresh(BUYER_C);
    if (!texts(recs).includes("That makes your order")) {
      const figs = figures(texts(recs)).filter((f) => f >= 310 && f < 350);
      const counter = figs[figs.length - 1];
      if (counter !== undefined) {
        await say(BUYER_C, String(counter));
        recs = await fresh(BUYER_C);
      }
    }
    check(texts(recs).includes("That makes your order"),
      "leg C setup: the hand is agreed at one total", texts(recs));
    await say(BUYER_C, "show me what's in my basket");
    recs = await fresh(BUYER_C);
    console.log("LEG C BASKET REPLY:\n" + texts(recs));
    check(texts(recs).includes("Thong Sandals - GH₵") && texts(recs).includes("Double Clothes Rack - GH₵"),
      "leg C: the agreed hand is listed item by item at its shares", texts(recs));

    // Leg D: the ask in the owner's own words — "Check
    // basket", no "my", no viewing phrase the net knows.
    // The net does not claim it; the thinking reads it and
    // the same view answers.
    recs = await pickBoth(BUYER_D);
    await say(BUYER_D, "Check basket");
    recs = await fresh(BUYER_D);
    console.log("LEG D BASKET REPLY:\n" + texts(recs));
    check(texts(recs).includes("Thong Sandals - GH₵80") && texts(recs).includes("Double Clothes Rack - GH₵270"),
      "leg D: 'Check basket' is read by the thinking and answered with the list", texts(recs));
  } finally {
    child.kill();
    fs.rmSync(dir, { recursive: true, force: true });
  }
  console.log(failures === 0 ? "PROOF GREEN" : `PROOF RED: ${failures} leg(s) failed`);
  process.exit(failures === 0 ? 0 : 1);
})();
