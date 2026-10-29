// Live proof for burst batching (burst.go), on the real
// model key, with real WhatsApp media ids supplied by the
// caller as BURST_MEDIA_1..3 (the same three sandal designs
// the merchant forwarded on the live number). A seller
// forwards three different products, each captioned with
// its sizes and price, while the first listing is open:
//   - leg 1: the first photo opens its listing; the second
//     and third are kept for the batch with one ack between
//     them;
//   - leg 2: the open listing saves on her answers, and the
//     batch is presented in the same turn — both items,
//     at GH₵80, with their sizes;
//   - leg 3: one reply, "all 75", prices and saves the
//     batch;
//   - audit: the safety invariants — every sent photo is
//     held exactly once across the shelf, and every product
//     is 80/75 with the captioned sizes. How the model
//     groups the photos into products is its judgement and
//     is reported, not asserted.
// Run:
//   BURST_MEDIA_1=.. BURST_MEDIA_2=.. BURST_MEDIA_3=.. \
//   META_MODEL_API_KEY=.. WHATSAPP_TOKEN=.. node scripts/prove-burst.js
"use strict";
const { spawn } = require("child_process");
const fs = require("fs");
const http = require("http");
const os = require("os");
const path = require("path");

const MEDIA = [process.env.BURST_MEDIA_1, process.env.BURST_MEDIA_2, process.env.BURST_MEDIA_3];
if (MEDIA.some((m) => !m) || !process.env.WHATSAPP_TOKEN) {
  console.log("SKIP: BURST_MEDIA_1..3 or WHATSAPP_TOKEN missing; the burst proof needs real media.");
  process.exit(0);
}
const ROOT = path.join(__dirname, "..");
const PORT = 3221;
const BASE = `http://127.0.0.1:${PORT}`;
const OWNER = "233551209901";
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "burst-"));
const cat = { merchants: { burst: { id: "burst", name: "Burst Shop", shopCode: "BURST", ownerWhatsApp: OWNER, payoutPhone: OWNER, products: [] } } };
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
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from: OWNER, id: "wb" + seq, type: "image", image: { id: imageId, caption } }] } }] }] });
}
function say(text) {
  seq++;
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from: OWNER, id: "wb" + seq, type: "text", text: { body: text } }] } }] }] });
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
      try {
        const ok = await new Promise((res) => {
          http.get(BASE + "/healthz", (r) => { let b = ""; r.on("data", (c) => (b += c)); r.on("end", () => res(b.includes('"ok":true'))); }).on("error", () => res(false));
        });
        if (ok) break;
      } catch (e) { /* not up yet */ }
      await sleep(500);
    }
    console.log("== leg 1: a burst of three different products, each captioned 37-41@80 ==");
    await photo(MEDIA[0], "37-41@80");
    const open = await waitFor((t) => /last price/i.test(t));
    const openText = open.map((r) => r.body || "").join("\n");
    check("the first photo opens its own listing and asks its last price", /Got it/.test(openText) && /last price/i.test(openText), openText.slice(0, 200));
    await photo(MEDIA[1], "37-41@80");
    await sleep(3000);
    await photo(MEDIA[2], "37-41@80");
    const kept = await fresh(90000);
    const keptText = kept.map((r) => r.body || "").join("\n");
    const keptLines = keptText.split("\n").filter((l) => /kept/i.test(l));
    check("the other two are kept for the batch, acknowledged once", keptLines.length >= 1 && keptLines.length <= 2, keptText.slice(0, 200));
    check("nothing was merged onto the open listing or saved yet", !/Saved|Added/.test(keptText), keptText.slice(0, 120));

    console.log("== leg 2: the open listing saves; the batch is presented whole ==");
    await say("75");
    let dialogue = "";
    for (let i = 0; i < 5; i++) {
      const got = await fresh(90000);
      dialogue += got.map((r) => r.body || "").join("\n") + "\n";
      if (/kept 2 more items/i.test(dialogue)) break;
      if (/by measure/i.test(dialogue) && !/Saved ✅/.test(dialogue)) { await say("whole"); }
    }
    check("the open listing saved", /Saved ✅/.test(dialogue), dialogue.slice(0, 200));
    check("the batch is presented with both items", /kept 2 more items/i.test(dialogue), dialogue.slice(0, 200));
    check("the presented items carry the captioned price", (dialogue.match(/GH₵80/g) || []).length >= 2, dialogue.slice(0, 300));
    check("the presented items carry their sizes", /37/.test(dialogue) && /41/.test(dialogue), dialogue.slice(0, 300));

    console.log("== leg 3: one reply prices and saves the whole batch ==");
    await say("all 75");
    // How many items the batch held depends on the model's
    // identity judgements in leg 1 (a photo it reads as the
    // open listing's own product joins that listing instead);
    // what must hold is that whatever was batched is saved by
    // this one reply.
    const saved = await waitFor((t) => (t.match(/Added ✅/g) || []).length >= 1);
    const savedText = saved.map((r) => r.body || "").join("\n");
    check("the batched items were added on the one reply", (savedText.match(/Added ✅/g) || []).length >= 1, savedText.slice(0, 200));

    console.log("== audit: the catalogue ==");
    await sleep(2000);
    const onDisk = JSON.parse(fs.readFileSync(path.join(dir, "catalogue.json"), "utf8"));
    const products = onDisk.merchants.burst.products;
    console.log("  products landed:", JSON.stringify(products.map((p) => p.name)));
    // The safety invariants: no photo lost, none held by two
    // products, and no price or size carried across items.
    check("at least two products landed", products.length >= 2, JSON.stringify(products.map((p) => p.name)));
    check("every product is 80 list, 75 last", products.every((p) => p.listPrice === 80 && p.floorPrice === 75), JSON.stringify(products.map((p) => [p.listPrice, p.floorPrice])));
    check("every sent photo is held exactly once across the shelf", products.flatMap((p) => p.photos || []).length === 3 && new Set(products.flatMap((p) => p.photos)).size === 3, JSON.stringify(products.map((p) => p.photos)));
    check("every product carries the captioned sizes", products.every((p) => (p.sizes || []).join(",").includes("37") && (p.sizes || []).join(",").includes("41")), JSON.stringify(products.map((p) => p.sizes)));
  } finally {
    child.kill("SIGTERM");
  }
  console.log(`\nBURST PROOF: ${pass} passed, ${fail} failed`);
  process.exit(fail ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(2); });
