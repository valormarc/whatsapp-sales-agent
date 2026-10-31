// Live proof for the shelf sheets and the tap-to-add
// picking, on the real model key, against a real spawned
// process (sends stay in dry-run: the sheet's Graph shape
// is pinned by the payload test against Meta's documented
// schema, and the on-phone sheet itself is the deploy's
// phone check):
//   - leg 1: a buyer entering by the shop's link is
//     greeted and shown Shelf 1 as a list sheet: nine
//     items and a More row, under the platform's limits;
//   - leg 2: tapping More opens Shelf 2 with the rest;
//   - leg 3: tapping an item adds it to her picks — the
//     sheet returns with the item ticked and a Done row
//     carrying the hand so far;
//   - leg 4: a second tap adds a second item, and the
//     Done row's count and total move with it;
//   - leg 5: tapping a picked item again takes it off;
//   - leg 6: the Done tap starts the settling on the
//     first pick, one by one;
//   - leg 7: settling the first pick parks it and brings
//     the next one up;
//   - leg 8: settling the last pick opens the one
//     fulfilment, and the whole journey completes as ONE
//     order carrying both items.
// Run:
//   META_MODEL_API_KEY=<redacted> node scripts/prove-shelf.js
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
const PORT = 3226;
const BASE = `http://127.0.0.1:${PORT}`;
const OWNER = "233551209001";
const BUYER = "233551209101";
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "shelf-"));
const products = [];
for (let i = 1; i <= 12; i++) {
  products.push({ id: "item-" + i, name: "Shelf Item " + i, description: "Proof item number " + i + ".", listPrice: 100 + i * 10, floorPrice: 90 + i * 10, stock: 2 });
}
const cat = { merchants: { shelf: { id: "shelf", name: "Shelf Shop", shopCode: "SHELF", ownerWhatsApp: OWNER, payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 25, deliveryNote: "Sent by bus to your station", products } } };
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
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from, id: "ws" + seq, type: "text", text: { body: text } }] } }] }] });
}
function tap(from, rowID, title) {
  seq++;
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from, id: "ws" + seq, type: "interactive", interactive: { type: "list_reply", list_reply: { id: rowID, title: title || "" } } }] } }] }] });
}
function outbox(to) {
  const f = path.join(dir, "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)).filter((r) => r.to === to);
}
let cursor = 0;
async function fresh(timeoutMs) {
  const end = Date.now() + (timeoutMs || 90000);
  while (Date.now() < end) {
    const all = outbox(BUYER);
    if (all.length > cursor) {
      const got = all.slice(cursor);
      cursor = all.length;
      await sleep(4000);
      const more = outbox(BUYER);
      if (more.length > cursor) { got.push(...more.slice(cursor)); cursor = more.length; }
      return got;
    }
    await sleep(1500);
  }
  return [];
}
const texts = (recs) => recs.map((r) => r.body || "").join("\n");
const sheetOf = (recs) => recs.find((r) => r.kind === "list");
const rowsOf = (sheet) => (sheet ? sheet.sections.flatMap((s) => s.rows) : []);
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
    env: { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: dir, WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1" },
    stdio: "ignore",
  });
  try {
    await waitHealth();

    await say(BUYER, "SHOP-SHELF");
    let recs = await fresh();
    const sheet1 = sheetOf(recs);
    check(!!sheet1, "leg 1: entry presents the shelf as a list sheet", texts(recs));
    if (sheet1) {
      const rows = sheet1.sections[0].rows;
      check(sheet1.sections[0].title === "Shelf 1 of 2" && rows.length === 10 && rows[9].id === "shelfmore_2",
        "leg 1: Shelf 1 carries nine items and the More row", JSON.stringify(sheet1.sections[0]).slice(0, 300));
      check(rows.slice(0, 9).every((r) => r.id.startsWith("shelf_item-") && r.title.length <= 24 && (r.description || "").length <= 72),
        "leg 1: every row sits inside the platform limits", JSON.stringify(rows).slice(0, 300));
    }

    await tap(BUYER, "shelfmore_2", "More items");
    recs = await fresh(60000);
    const sheet2 = sheetOf(recs);
    check(!!sheet2 && sheet2.sections[0].title === "Shelf 2 of 2" && sheet2.sections[0].rows.length === 3,
      "leg 2: the More tap opens Shelf 2 with the remaining items", texts(recs));

    await tap(BUYER, "shelf_item-11", "Shelf Item 11");
    recs = await fresh(60000);
    const sheet3 = sheetOf(recs);
    const done3 = rowsOf(sheet3).find((r) => r.id === "shelfdone");
    check(!!sheet3 && texts(recs).includes("Added the Shelf Item 11") && !!done3 && done3.description.includes("1 item") && done3.description.includes("GH₵210"),
      "leg 3: a tap adds the item and the Done row carries the hand so far", texts(recs) + JSON.stringify(sheet3 || {}).slice(0, 200));
    check(rowsOf(sheet3).some((r) => r.id === "shelf_item-11" && r.title.startsWith("✓")),
      "leg 3: the picked item wears its tick", JSON.stringify(rowsOf(sheet3)).slice(0, 300));

    await tap(BUYER, "shelf_item-12", "Shelf Item 12");
    recs = await fresh(60000);
    const sheet4 = sheetOf(recs);
    const done4 = rowsOf(sheet4).find((r) => r.id === "shelfdone");
    check(texts(recs).includes("2 items picked so far, GH₵430 in all") && !!done4 && done4.description.includes("2 items"),
      "leg 4: a second tap adds a second item and the figures move", texts(recs));

    await tap(BUYER, "shelf_item-12", "Shelf Item 12");
    recs = await fresh(60000);
    const done5 = rowsOf(sheetOf(recs)).find((r) => r.id === "shelfdone");
    check(texts(recs).includes("Took the Shelf Item 12 off your picks") && !!done5 && done5.description.includes("1 item"),
      "leg 5: tapping a picked item again takes it off", texts(recs));

    await tap(BUYER, "shelf_item-12", "Shelf Item 12");
    recs = await fresh(60000);
    check(texts(recs).includes("Added the Shelf Item 12"), "leg 6 setup: the item goes back on", texts(recs));

    await tap(BUYER, "shelfdone", "Done, settle them");
    recs = await fresh(60000);
    check(texts(recs).includes("one by one") && texts(recs).includes("Shelf Item 11") && texts(recs).includes("GH₵210"),
      "leg 6: Done starts the settling on the first pick", texts(recs));

    await say(BUYER, "210");
    recs = await fresh();
    check(texts(recs).includes("yours for GH₵210") && texts(recs).includes("Now the next one you picked:") && texts(recs).includes("Shelf Item 12") && texts(recs).includes("GH₵220"),
      "leg 7: settling the first pick parks it and brings the next up", texts(recs));

    await say(BUYER, "220");
    recs = await fresh();
    check(texts(recs).includes("yours for GH₵220") && texts(recs).includes("That makes your order GH₵430 in all.") && texts(recs).includes("How do you want to get it?"),
      "leg 8: settling the last pick opens the one fulfilment for the whole order", texts(recs));

    await say(BUYER, "Send it to Accra");
    recs = await fresh();
    check(texts(recs).includes("What name should go on the package?"), "leg 8: the package name is asked once for the whole order", texts(recs));
    await say(BUYER, "Efua Mensima");
    recs = await fresh();
    await say(BUYER, "no");
    recs = await fresh();
    await say(BUYER, "yes");
    recs = await fresh();
    const ordersFile = path.join(dir, "orders.json");
    let orderOK = false, orderEvidence = "no orders file";
    if (fs.existsSync(ordersFile)) {
      const book = JSON.parse(fs.readFileSync(ordersFile, "utf8"));
      const all = Object.values(book);
      orderEvidence = JSON.stringify(all).slice(0, 300);
      orderOK = all.length === 1 && all[0].items && all[0].items.length === 2 && all[0].price === 430;
    }
    check(orderOK, "leg 8: one order in the book carries both items at GH₵430", orderEvidence);
  } finally {
    child.kill("SIGTERM");
  }
  console.log(failures === 0 ? "PROOF GREEN" : "PROOF RED: " + failures + " leg(s) failed");
  process.exit(failures === 0 ? 0 : 1);
})().catch((e) => { console.log("FAIL: " + e.message); process.exit(1); });
