// Live battery for the listing rule on the discovery
// surfaces: a cross-shop search, the shops directory and
// the sampler ("taste") must come back as lists — items
// one per line under their shop — never strung along a
// line or into a sentence. The basket legs of the rule
// are proven by prove-basketview.js; the payment record's
// multi-item list is pinned exactly in the Go suite (a
// record's paper is deterministic). Run:
//   META_MODEL_API_KEY=<key> node scripts/prove-listing.js
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
const PORT = 3245;
const BASE = `http://127.0.0.1:${PORT}`;
const BUYER_1 = "233551209811";
const BUYER_2 = "233551209812";
const BUYER_3 = "233551209813";
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "listing-"));
const cat = { merchants: {
  luxe: { id: "luxe", name: "Luxe", shopCode: "LUXE", ownerWhatsApp: "233551209801", payoutPhone: "233551209801", dailyGiveawayAllowance: 300, deliveryFee: 25, deliveryNote: "Sent by bus to your station", products: [
    { id: "sandals", name: "Thong Sandals", description: "Thong sandals, sizes 37-41.", listPrice: 80, floorPrice: 60, stock: 5 },
    { id: "rack", name: "Double Clothes Rack", description: "Black metal double clothes rack.", listPrice: 270, floorPrice: 250, stock: 4 },
  ] },
  adom: { id: "adom", name: "Adom Fashion", shopCode: "ADOM", ownerWhatsApp: "233551209802", payoutPhone: "233551209802", dailyGiveawayAllowance: 300, deliveryFee: 20, deliveryNote: "Rider within Accra", products: [
    { id: "bsandals", name: "Beaded Sandals", description: "Hand-beaded sandals.", listPrice: 120, floorPrice: 100, stock: 3 },
    { id: "dress", name: "Ankara Dress", description: "Ankara print dress.", listPrice: 300, floorPrice: 260, stock: 2 },
  ] },
} };
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
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from, id: "wls" + seq, type: "text", text: { body: text } }] } }] }] });
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

    // The cross-shop search: sandals live in both shops.
    await say(BUYER_1, "I am looking for sandals");
    let recs = await fresh(BUYER_1);
    console.log("SEARCH REPLY:\n" + texts(recs));
    check(texts(recs).includes("Thong Sandals: GH₵80") && texts(recs).includes("Beaded Sandals: GH₵120"),
      "search: both shops' sandals are listed at their prices", texts(recs));
    check(!texts(recs).includes(" | ") && !/has: [^\n]* - GH₵/.test(texts(recs)),
      "search: nothing is strung along one line", texts(recs));

    // The directory: which shops are here at all.
    await say(BUYER_2, "What shops do you have?");
    recs = await fresh(BUYER_2);
    console.log("DIRECTORY REPLY:\n" + texts(recs));
    check(!/\(SHOP-[A-Z]+\): [^\n]*,/.test(texts(recs)),
      "directory: no shop's goods are strung on its line", texts(recs));

    // The sampler: a want nobody stocks.
    await say(BUYER_3, "Do you sell iPhones?");
    recs = await fresh(BUYER_3);
    console.log("TASTE REPLY:\n" + texts(recs));
    check(texts(recs).includes("Here is some of what the shops have:\n") && !texts(recs).includes("; "),
      "taste: the sampler is a list, not a sentence", texts(recs));
  } finally {
    child.kill();
    fs.rmSync(dir, { recursive: true, force: true });
  }
  console.log(failures === 0 ? "PROOF GREEN" : `PROOF RED: ${failures} leg(s) failed`);
  process.exit(failures === 0 ? 0 : 1);
})();
