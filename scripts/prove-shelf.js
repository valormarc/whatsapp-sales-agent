// Live proof for the shelf sheets, on the real model
// key, against a real spawned process (sends stay in
// dry-run: the sheet's Graph shape is pinned by the
// payload test against Meta's documented schema, and the
// on-phone sheet itself is the deploy's phone check):
//   - leg 1: a buyer entering by the shop's link is
//     greeted and shown Shelf 1 as a list sheet: nine
//     items and a More row, under the platform's limits;
//   - leg 2: tapping More opens Shelf 2 with the rest;
//   - leg 3: tapping an item puts it in front of her —
//     its name and its price — the way naming it does;
//   - leg 4: a bare figure after the tap is read as her
//     offer on that item (the natural-offers road), so a
//     tap truly is a pick, not a picture.
// Run:
//   META_MODEL_API_KEY=<redacted> node scripts/prove-shelf.js
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
const PORT = 3226;
const BASE = `http://127.0.0.1:${PORT}`;
const OWNER = "233551209001";
const BUYER = "233551209101";
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "shelf-"));
const products = [];
for (let i = 1; i <= 12; i++) {
  products.push({ id: "item-" + i, name: "Shelf Item " + i, description: "Proof item number " + i + ".", listPrice: 100 + i * 10, floorPrice: 90 + i * 10, stock: 2 });
}
const cat = { merchants: { shelf: { id: "shelf", name: "Shelf Shop", shopCode: "SHELF", ownerWhatsApp: OWNER, payoutPhone: OWNER, dailyGiveawayAllowance: 300, products } } };
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

    await say(BUYER, "SHOP-SHELF");
    let recs = await fresh();
    const sheet1 = recs.find((r) => r.kind === "list");
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
    const sheet2 = recs.find((r) => r.kind === "list");
    check(!!sheet2 && sheet2.sections[0].title === "Shelf 2 of 2" && sheet2.sections[0].rows.length === 3,
      "leg 2: the More tap opens Shelf 2 with the remaining items", texts(recs));

    await tap(BUYER, "shelf_item-11", "Shelf Item 11");
    recs = await fresh(60000);
    check(texts(recs).includes("Shelf Item 11") && texts(recs).includes("GH₵210"),
      "leg 3: an item tap presents the item and its price", texts(recs));

    await say(BUYER, "180");
    recs = await fresh();
    check(texts(recs).includes("GH₵") && !/which|what item|not sure/i.test(texts(recs)),
      "leg 4: a bare figure after the tap is read as her offer on that item", texts(recs));
  } finally {
    child.kill("SIGTERM");
  }
  console.log(failures === 0 ? "PROOF GREEN" : "PROOF RED: " + failures + " leg(s) failed");
  process.exit(failures === 0 ? 0 : 1);
})().catch((e) => { console.log("FAIL: " + e.message); process.exit(1); });
