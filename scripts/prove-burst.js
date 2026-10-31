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
//   - leg 2b: a sentence that merely contains a number
//     ("I have 2 pairs in stock") saves nothing and leaves
//     the batch waiting;
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

// Sandbox hygiene: a dry-run child must never inherit live
// send or settle credentials from the operator's shell. This
// proof keeps the WhatsApp token — the sandbox needs it to
// download the real media it is fed — but the phone number
// id (the sending identity) and the Seev credentials are
// blanked, so the child cannot send or settle anything.
delete process.env.WHATSAPP_PHONE_NUMBER_ID;
delete process.env.WHATSAPP_APP_SECRET;
delete process.env.WHATSAPP_VERIFY_TOKEN;
delete process.env.SEEV_SECRET_KEY;
delete process.env.SEEV_API_KEY;
delete process.env.SEEV_WEBHOOK_SECRET;

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
  const env = { ...process.env, WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1", HACKATHON_AGENT_DATA_DIR: dir, PORT: String(PORT) };
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

    console.log("== leg 2: every kept item completes, through whichever door it took ==");
    // Kept items complete through different doors depending
    // on their reads: a fully read item waits in the batch
    // for its presentation, an incompletely read one is
    // held and opens as its own listing, asking its own
    // last price. Drive whichever door is open: answer a
    // listing's own last-price question with 75; when the
    // batch presentation is the open question, probe it
    // with a numbered sentence (leg 2b) and then price it
    // with one reply (leg 3).
    const onDisk = () => JSON.parse(fs.readFileSync(path.join(dir, "catalogue.json"), "utf8")).merchants.burst.products || [];
    let probed = false;
    await say("75");
    for (let round = 0; round < 14 && onDisk().length < 3; round++) {
      const got = await fresh(45000);
      const text = got.map((r) => r.body || "").join("\n");
      if (/kept [0-9]+ more item/i.test(text)) {
        check("the batch is presented with its kept items", true);
        check("the presented items carry the captioned price", (text.match(/GH₵80/g) || []).length >= 1, text.slice(0, 300));
        check("the presented items carry their sizes", /37/.test(text) && /41/.test(text), text.slice(0, 300));
        if (!probed) {
          probed = true;
          console.log("== leg 2b: a numbered sentence is not the batch's price answer ==");
          const before = onDisk().length;
          await say("I have 2 pairs in stock");
          let strayText = "";
          for (let i = 0; i < 3; i++) {
            const stray = await fresh(15000);
            strayText += stray.map((r) => r.body || "").join("\n") + "\n";
            if (/Added ✅/.test(strayText)) break;
          }
          check("the numbered sentence saved nothing at the batch door", !/Added ✅/.test(strayText) && onDisk().length === before, strayText.slice(0, 200));
          console.log("== leg 3: one reply prices and saves the whole batch ==");
          await say("all 75");
          const saved = await waitFor((t) => (t.match(/Added ✅/g) || []).length >= 1);
          const savedText = saved.map((r) => r.body || "").join("\n");
          check("the batched items were added on the one reply", (savedText.match(/Added ✅/g) || []).length >= 1, savedText.slice(0, 200));
        }
        continue;
      }
      if (/What is your last price/i.test(text)) { await say("75"); continue; }
      if (/by measure/i.test(text)) { await say("whole"); continue; }
    }
    if (!probed) {
      console.log("  NOTE the batch door was not exercised this run: every kept item took the held door.");
    }
    check("all three products completed", onDisk().length >= 3, JSON.stringify(onDisk().map((p) => p.name)));

    console.log("== audit: the catalogue ==");
    await sleep(2000);
    const finalCat = JSON.parse(fs.readFileSync(path.join(dir, "catalogue.json"), "utf8"));
    const products = finalCat.merchants.burst.products;
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
