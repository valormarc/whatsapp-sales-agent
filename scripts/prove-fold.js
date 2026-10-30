// Live proof for the burst fold's identity verdict
// (burst.go), on the real model key, with real WhatsApp
// media ids supplied by the caller as FOLD_MEDIA_1..4:
// a rack photo (the listing that opens), two angles of a
// halter-neck pleated maxi dress, and a denim mini dress.
// The three dress photos are all captioned identically —
// the same name at the same price, "Summer Dress" at
// GH₵150, no description line, so every description in
// play is the model's own read of a photo. On words alone
// all three fold into one item and two products are lost;
// the fold now takes the model's identity verdict against
// the kept item first, judged against what the model's
// own read of that item's photo said it is.
//   - leg 1: the rack opens its listing; the first dress
//     is kept for the batch;
//   - leg 2: the denim mini — a different construction
//     under the same name at the same price — is kept as
//     its OWN item, not folded into the dress;
//   - leg 3: the second angle of the halter dress still
//     folds into it, as an angle should;
//   - leg 4: the rack saves, the batch presents TWO
//     dresses, "140, 140" prices them, and the shelf
//     holds two dress products: one carrying both halter
//     photos, one carrying the denim photo alone.
// The verdict is the model's judgement; this proof uses
// two dresses no reader could confuse and reports what
// the model did. (An earlier run of this proof used two
// sandal designs whose own vision reads came back
// interchangeable — both "strappy with studs" — and the
// model called them the same product; that pair could
// not test discrimination and is not used here.)
// Run:
//   FOLD_MEDIA_1=.. FOLD_MEDIA_2=.. FOLD_MEDIA_3=.. \
//   FOLD_MEDIA_4=.. META_MODEL_API_KEY=.. \
//   WHATSAPP_TOKEN=.. node scripts/prove-fold.js
"use strict";
const { spawn } = require("child_process");
const fs = require("fs");
const http = require("http");
const os = require("os");
const path = require("path");

const MEDIA = [process.env.FOLD_MEDIA_1, process.env.FOLD_MEDIA_2, process.env.FOLD_MEDIA_3, process.env.FOLD_MEDIA_4];
if (MEDIA.some((m) => !m) || !process.env.WHATSAPP_TOKEN || !process.env.META_MODEL_API_KEY) {
  console.log("SKIP: FOLD_MEDIA_1..4, WHATSAPP_TOKEN or META_MODEL_API_KEY missing; the fold proof needs real media and the real key.");
  process.exit(0);
}
const ROOT = path.join(__dirname, "..");
const PORT = 3223;
const BASE = `http://127.0.0.1:${PORT}`;
const OWNER = "233551209951";
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "fold-"));
const cat = { merchants: { fold: { id: "fold", name: "Fold Shop", shopCode: "FOLD", ownerWhatsApp: OWNER, payoutPhone: OWNER, products: [] } } };
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
function photo(imageId, caption) {
  seq++;
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from: OWNER, id: "wd" + seq, type: "image", image: { id: imageId, caption } }] } }] }] });
}
function say(text) {
  seq++;
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from: OWNER, id: "wd" + seq, type: "text", text: { body: text } }] } }] }] });
}
function outbox() {
  const f = path.join(dir, "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)).filter((r) => r.to === OWNER);
}
let cursor = 0;
async function fresh(timeoutMs) {
  const end = Date.now() + (timeoutMs || 120000);
  let got = [];
  while (Date.now() < end) {
    const all = outbox();
    if (all.length > cursor) {
      got = all.slice(cursor);
      cursor = all.length;
      await sleep(6000);
      const more = outbox();
      if (more.length > cursor) { got = got.concat(more.slice(cursor)); cursor = more.length; }
      return got;
    }
    await sleep(500);
  }
  return got;
}
async function waitFor(pred, timeoutMs) {
  const end = Date.now() + (timeoutMs || 120000);
  let seen = [];
  while (Date.now() < end) {
    const got = await fresh(20000);
    seen = seen.concat(got);
    if (pred(seen.map((r) => r.body || "").join("\n"))) return seen;
  }
  return seen;
}

let pass = 0, fail = 0;
function check(name, ok, detail) {
  if (ok) { pass++; console.log("  PASS", name); }
  else { fail++; console.log("  FAIL", name, detail || ""); }
}

async function main() {
  const env = { ...process.env, WHATSAPP_DRY_RUN: "1", HACKATHON_AGENT_DATA_DIR: dir, PORT: String(PORT) };
  delete env.SEEV_API_KEY; delete env.SEEV_SECRET_KEY; delete env.SEEV_WEBHOOK_SECRET;
  const child = spawn(path.join(ROOT, "agent"), [], { cwd: ROOT, env, stdio: "ignore" });
  try {
    for (let i = 0; i < 40; i++) {
      const ok = await new Promise((res) => {
        http.get(BASE + "/healthz", (r) => { let b = ""; r.on("data", (c) => (b += c)); r.on("end", () => res(b.includes('"ok":true'))); }).on("error", () => res(false));
      });
      if (ok) break;
      await sleep(500);
    }
    console.log("== leg 1: a rack opens; the halter dress is kept ==");
    await photo(MEDIA[0], "Corner Rack\nSteel frame with 4 tiers\nGH₵270");
    const open = await waitFor((t) => /last price/i.test(t));
    const openText = open.map((r) => r.body || "").join("\n");
    check("the rack opens its own listing and asks its last price", /Got it/.test(openText) && /last price/i.test(openText), openText.slice(0, 200));
    await photo(MEDIA[1], "Summer Dress\nGH₵150");
    const kept1 = await waitFor((t) => /kept/i.test(t), 90000);
    check("the halter dress is kept for the batch", /kept/i.test(kept1.map((r) => r.body || "").join("\n")), kept1.map((r) => r.body || "").join("\n").slice(0, 200));

    console.log("== leg 2: the denim mini under the same name and price is kept apart ==");
    await photo(MEDIA[2], "Summer Dress\nGH₵150");
    // A kept-as-its-own item and a folded one can both pass
    // without a fresh ack (acks are throttled per burst), so
    // this leg only waits for the turn to settle; the
    // presentation in leg 4 is where the count is proven.
    await fresh(90000);

    console.log("== leg 3: the halter dress's second angle still folds into it ==");
    await photo(MEDIA[3], "Summer Dress\nGH₵150");
    await fresh(90000);

    console.log("== leg 4: the batch presents two dresses; both save, photos unmixed ==");
    await say("250");
    let presented = "";
    for (let round = 0; round < 10 && !/kept [0-9]+ more item/i.test(presented); round++) {
      const got = await fresh(45000);
      const text = got.map((r) => r.body || "").join("\n");
      presented += "\n" + text;
      if (/by measure/i.test(text)) { await say("whole"); continue; }
      if (/What is your last price/i.test(text)) { await say("250"); continue; }
    }
    check("the batch is presented with its kept items", /kept [0-9]+ more item/i.test(presented), presented.slice(0, 300));
    check("both dresses are presented as their own items", /kept 2 more items/i.test(presented) && (presented.match(/Summer Dress/g) || []).length >= 2, presented.slice(0, 400));

    await say("140, 140");
    const saved = await waitFor((t) => (t.match(/Added ✅/g) || []).length >= 2, 90000);
    const savedText = saved.map((r) => r.body || "").join("\n");
    check("both dresses were added on the one reply", (savedText.match(/Added ✅/g) || []).length >= 2, savedText.slice(0, 300));

    const finalCat = JSON.parse(fs.readFileSync(path.join(dir, "catalogue.json"), "utf8"));
    const products = finalCat.merchants.fold.products;
    console.log("  products landed:", JSON.stringify(products.map((p) => [p.name, (p.photos || []).length])));
    const dresses = products.filter((p) => p.listPrice === 150);
    check("the shelf holds two dress products", dresses.length === 2, JSON.stringify(products.map((p) => p.name)));
    const counts = dresses.map((p) => (p.photos || []).length).sort();
    check("one dress carries both halter photos, the other the denim photo alone", dresses.length === 2 && counts[0] === 1 && counts[1] === 2 && new Set(dresses.flatMap((p) => p.photos || [])).size === 3, JSON.stringify(dresses.map((p) => p.photos)));
    check("both dresses are 150 list, 140 last", dresses.length === 2 && dresses.every((p) => p.floorPrice === 140), JSON.stringify(dresses.map((p) => [p.listPrice, p.floorPrice])));
  } finally {
    child.kill("SIGTERM");
  }
  console.log(`\nFOLD PROOF: ${pass} passed, ${fail} failed`);
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(2); });
