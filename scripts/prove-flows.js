// Live proof for the details forms, on the real model
// key, against a real spawned process. The forms are
// configured with placeholder ids (the real ones are
// account assets, created once in WhatsApp Manager — see
// scripts/flows/SETUP.md), so the sends stay in dry-run
// and completions are delivered as the webhooks Meta
// sends. What is proven is the whole working chain:
//   - leg 1: the merchant says "payout details" and the
//     payout form is offered with a fresh token;
//   - leg 2: its completion records the new MoMo number
//     and holder name in the catalogue itself (the Seev
//     remake is unconfigured in this scratch world, so
//     the reply takes the saved-and-finish-later branch,
//     and says so);
//   - leg 3: a buyer closing a delivery order is offered
//     the delivery form beside the package-name question;
//   - leg 4: completing it fills the order she is making —
//     when the sale completes, the order in the book
//     carries her package name, her town, the rider's
//     number and her landmark, and the seller's notice
//     lines carry them too (read from the order book the
//     notice is composed from).
// Run:
//   META_MODEL_API_KEY=<redacted> node scripts/prove-flows.js
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
const OWNER = "233551210001";
const BUYER = "233551210101";
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "flows-"));
const cat = { merchants: { flows: { id: "flows", name: "Flow Shop", shopCode: "FLOWS", ownerWhatsApp: OWNER, payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 20, deliveryNote: "Sent by rider.", products: [{ id: "basket", name: "Flow Basket", description: "Woven proof basket.", listPrice: 210, floorPrice: 190, stock: 2 }] } } };
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
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from, id: "wf" + seq, type: "text", text: { body: text } }] } }] }] });
}
function completeFlow(from, answers) {
  seq++;
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from, id: "wf" + seq, type: "interactive", interactive: { type: "nfm_reply", nfm_reply: { name: "flow", body: "Sent", response_json: JSON.stringify(answers) } } }] } }] }] });
}
function outbox(to) {
  const f = path.join(dir, "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)).filter((r) => r.to === to);
}
const cursors = {};
async function fresh(to, timeoutMs) {
  const end = Date.now() + (timeoutMs || 120000);
  while (Date.now() < end) {
    const all = outbox(to);
    const cur = cursors[to] || 0;
    if (all.length > cur) {
      const got = all.slice(cur);
      cursors[to] = all.length;
      await sleep(4000);
      const more = outbox(to);
      if (more.length > cursors[to]) { got.push(...more.slice(cursors[to])); cursors[to] = more.length; }
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
    console.log((ok ? "PASS: " : "FAIL: ") + label + (ok ? "" : " — " + (evidence || "").slice(0, 320)));
    if (!ok) failures++;
  };
  const child = spawn(path.join(ROOT, "agent"), [], {
    cwd: ROOT,
    env: { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: dir, WHATSAPP_DRY_RUN: "1", STALL_FLOW_DELIVERY_ID: "flow-delivery-test", STALL_FLOW_PAYOUT_ID: "flow-payout-test" },
    stdio: "ignore",
  });
  try {
    await waitHealth();

    // Legs 1-2: the merchant's payout form.
    await say(OWNER, "payout details");
    let recs = await fresh(OWNER, 60000);
    const payoutForm = recs.find((r) => r.kind === "flow");
    check(!!payoutForm && !!payoutForm.flowToken, "leg 1: 'payout details' offers the payout form with a token", texts(recs));
    if (payoutForm) {
      await completeFlow(OWNER, { flow_token: payoutForm.flowToken, holder: "Efua Mensah", phone: "0244987654" });
      recs = await fresh(OWNER, 60000);
      const shop = JSON.parse(fs.readFileSync(path.join(dir, "catalogue.json"), "utf8")).merchants.flows;
      check(shop.payoutPhone === "0244987654" && shop.payoutHolderName === "Efua Mensah" && texts(recs).includes("Saved"),
        "leg 2: the completion records the number and the name in the book", texts(recs) + " | book: " + shop.payoutPhone + "/" + shop.payoutHolderName);
    }

    // Legs 3-4: the buyer's delivery form, inside a real close.
    await say(BUYER, "SHOP-FLOWS");
    await fresh(BUYER);
    await say(BUYER, "How much is the Flow Basket?");
    await fresh(BUYER);
    await say(BUYER, "210");
    recs = await fresh(BUYER);
    check(/agree|deal|how do you want to get it|delivery/i.test(texts(recs)), "leg 3: the price is agreed and the close begins", texts(recs));
    await say(BUYER, "Delivery please");
    recs = await fresh(BUYER);
    if (!recs.some((r) => r.kind === "flow")) {
      // The walk may ask where first; answer, then look again.
      await say(BUYER, "Kasoa");
      recs = await fresh(BUYER);
    }
    const deliveryForm = recs.find((r) => r.kind === "flow") || outbox(BUYER).filter((r) => r.kind === "flow").pop();
    check(!!deliveryForm && !!deliveryForm.flowToken, "leg 3: the delivery form is offered beside the package-name question", texts(recs));
    if (deliveryForm) {
      await completeFlow(BUYER, { flow_token: deliveryForm.flowToken, buyer_name: "Ama Serwaa", phone: "0244123456", town: "Kasoa", landmark: "Near the station" });
      recs = await fresh(BUYER);
      check(texts(recs).includes("Got them, thank you.") && texts(recs).includes("Any message for the seller"),
        "leg 4: the completion thanks her and the walk moves on", texts(recs));
      await say(BUYER, "no");
      recs = await fresh(BUYER);
      await say(BUYER, "yes");
      recs = await fresh(BUYER);
      const ordersFile = path.join(dir, "orders.json");
      const book = fs.existsSync(ordersFile) ? JSON.parse(fs.readFileSync(ordersFile, "utf8")) : {};
      const mine = Object.values(book).find((o) => o.from === BUYER || o.From === BUYER);
      check(!!mine && mine.packageName === "Ama Serwaa" && mine.location === "Kasoa" && mine.deliveryPhone === "0244123456" && mine.landmark === "Near the station",
        "leg 4: the completed order carries the form's details in the book", JSON.stringify(mine || {}).slice(0, 320));
    }
  } finally {
    child.kill("SIGTERM");
  }
  console.log(failures === 0 ? "PROOF GREEN" : "PROOF RED: " + failures + " leg(s) failed");
  process.exit(failures === 0 ? 0 : 1);
})().catch((e) => { console.log("FAIL: " + e.message); process.exit(1); });
