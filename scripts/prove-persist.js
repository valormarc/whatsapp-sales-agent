// Live proof for the two essay takes, on the real model
// key, against a real spawned process:
//   - leg 1: a seller's photo opens a listing and the walk
//     stops at its last-price question;
//   - leg 2: the process is stopped with SIGTERM, the way
//     the platform stops it, and leaves a sessions.json
//     snapshot behind;
//   - leg 3: the process starts again on the same data
//     directory, and the walk resumes from its own next
//     step: her "250" (and "whole" if the measure is asked)
//     saves the listing at 270/250, which no fresh session
//     could do;
//   - leg 4: she asks "what needs my attention today?" and
//     the answer is the composed list from her books: the
//     paid order waiting to be sent and the order waiting
//     on its buyer, with the day's figures — the model
//     classifies the question, the code composes the facts.
// Run:
//   PERSIST_MEDIA_1=.. META_MODEL_API_KEY=<redacted> \
//   WHATSAPP_TOKEN=.. node scripts/prove-persist.js
"use strict";
const { spawn } = require("child_process");
const fs = require("fs");
const http = require("http");
const os = require("os");
const path = require("path");

if (!process.env.PERSIST_MEDIA_1 || !process.env.WHATSAPP_TOKEN || !process.env.META_MODEL_API_KEY) {
  console.log("SKIP: PERSIST_MEDIA_1, WHATSAPP_TOKEN or META_MODEL_API_KEY missing.");
  process.exit(0);
}
const ROOT = path.join(__dirname, "..");
const PORT = 3223;
const BASE = `http://127.0.0.1:${PORT}`;
const OWNER = "233551207701";
const BUYER = "233551207702";
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "persist-"));
const cat = { merchants: { persist: { id: "persist", name: "Persist Shop", shopCode: "PERSIST", ownerWhatsApp: OWNER, payoutPhone: OWNER, dailyGiveawayAllowance: 300, products: [] } } };
fs.writeFileSync(path.join(dir, "catalogue.json"), JSON.stringify(cat));
const now = new Date().toISOString();
const orders = {
  "PERSIST-9001": { reference: "PERSIST-9001", status: "paid", from: BUYER, productName: "Proof Basket", price: 300, total: 300, merchantId: "persist", createdAt: now },
  "PERSIST-9002": { reference: "PERSIST-9002", status: "pending", from: BUYER, productName: "Proof Basket", price: 150, total: 150, merchantId: "persist", createdAt: now },
};
fs.writeFileSync(path.join(dir, "orders.json"), JSON.stringify(orders));

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
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from: OWNER, id: "wp" + seq, type: "image", image: { id: imageId, caption } }] } }] }] });
}
function say(text) {
  seq++;
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [{ from: OWNER, id: "wp" + seq, type: "text", text: { body: text } }] } }] }] });
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
      await sleep(5000);
      const more = outbox();
      if (more.length > cursor) { got = more.slice(0); cursor = more.length; }
      return got.map((r) => r.text || r.body || "").join("\n");
    }
    await sleep(1500);
  }
  return got.map((r) => r.text || r.body || "").join("\n");
}
function startAgent() {
  const child = spawn(path.join(ROOT, "agent"), [], {
    cwd: ROOT,
    env: {
      ...process.env,
      PORT: String(PORT),
      HACKATHON_AGENT_DATA_DIR: dir,
      WHATSAPP_DRY_RUN: "1",
    },
    stdio: "ignore",
  });
  return child;
}
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
function catalogue() {
  return JSON.parse(fs.readFileSync(path.join(dir, "catalogue.json"), "utf8"));
}

(async () => {
  let failures = 0;
  const check = (ok, label, evidence) => {
    console.log((ok ? "PASS: " : "FAIL: ") + label + (ok ? "" : " — " + evidence));
    if (!ok) failures++;
  };

  let child = startAgent();
  await waitHealth();
  await photo(process.env.PERSIST_MEDIA_1, "GH₵270");
  let got = await fresh();
  check(/last price/i.test(got), "leg 1: the photo opens a listing and asks its last price", got.slice(0, 300));

  child.kill("SIGTERM");
  await new Promise((r) => child.on("exit", r));
  await sleep(500);
  check(fs.existsSync(path.join(dir, "sessions.json")), "leg 2: the stopped process leaves a session snapshot", fs.readdirSync(dir).join(","));

  child = startAgent();
  await waitHealth();
  await say("250");
  got = await fresh(60000);
  let products = catalogue().merchants.persist.products;
  if (!products.some((p) => p.listPrice === 270)) {
    await say("whole");
    await fresh(60000);
    products = catalogue().merchants.persist.products;
  }
  const saved = products.find((p) => p.listPrice === 270);
  check(!!saved && saved.floorPrice === 250, "leg 3: the restarted process resumes the walk and saves the listing at 270/250", JSON.stringify(products).slice(0, 300));

  await say("what needs my attention today?");
  got = await fresh();
  check(got.includes("PERSIST-9001") && got.includes("waiting to be sent"), "leg 4: the attention answer names the paid order waiting to be sent", got.slice(0, 400));
  check(got.includes("waiting for buyers to pay") && got.includes("GH₵150"), "leg 4: the attention answer counts the order waiting on its buyer", got.slice(0, 400));
  check(got.includes("Today so far:"), "leg 4: the attention answer closes with the day's figures", got.slice(0, 400));

  child.kill("SIGTERM");
  console.log(failures === 0 ? "PROOF GREEN" : "PROOF RED: " + failures + " leg(s) failed");
  process.exit(failures === 0 ? 0 : 1);
})().catch((e) => { console.log("FAIL: " + e.message); process.exit(1); });
