// Live proof for the calm-chat fixes, on the real model
// key, against a real spawned process:
//   - leg 1: the owner asks "What should I call you?" and
//     the answer carries the agent's name, Stall;
//   - leg 2: a laughing emoji is answered like one — a
//     short reply with no figures in it, not the day book;
//   - leg 3: a day question gets the day answer with the
//     allowance reminder riding it (her allowance was
//     never chosen);
//   - leg 4: the same question again gets the day answer
//     WITHOUT the reminder — once a day is enough;
//   - leg 5: she answers the reminder by quoting it and
//     saying "set it 300" in her own words, and the
//     allowance is actually set in the book — the exact
//     flow that once drew a drafted "Done" over an
//     unchanged book;
//   - leg 6: a bare "Ok" is met with a nod, not a report.
// Run:
//   META_MODEL_API_KEY=<redacted> node scripts/prove-calm.js
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
const PORT = 3225;
const BASE = `http://127.0.0.1:${PORT}`;
const OWNER = "233551208801";
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "calm-"));
const cat = { merchants: { calm: { id: "calm", name: "Calm Shop", shopCode: "CALM", ownerWhatsApp: OWNER, payoutPhone: OWNER, products: [{ id: "basket", name: "Proof Basket", description: "Woven proof basket.", listPrice: 300, floorPrice: 250, stock: 2 }] } } };
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
function say(text, contextID) {
  seq++;
  const msg = { from: OWNER, id: "wc" + seq, type: "text", text: { body: text } };
  if (contextID) msg.context = { from: "233209450786", id: contextID };
  return post("/webhook", { entry: [{ changes: [{ value: { messages: [msg] } }] }] });
}
function outbox() {
  const f = path.join(dir, "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)).filter((r) => r.to === OWNER);
}
let cursor = 0;
async function fresh(timeoutMs) {
  const end = Date.now() + (timeoutMs || 120000);
  while (Date.now() < end) {
    const all = outbox();
    if (all.length > cursor) {
      const got = all.slice(cursor);
      cursor = all.length;
      await sleep(5000);
      const more = outbox();
      if (more.length > cursor) { got.push(...more.slice(cursor)); cursor = more.length; }
      return got.map((r) => r.body || "").join("\n");
    }
    await sleep(1500);
  }
  return "";
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

    await say("What should I call you?");
    let got = await fresh();
    check(got.includes("Stall"), "leg 1: the agent answers its name", got);

    await say("😂");
    got = await fresh(60000);
    check(got !== "" && !got.includes("sale(s)") && !got.includes("GH₵"), "leg 2: a laugh is met with a nod, not the day book", got);

    // The day answer's wording is the model's own on the
    // live path ("Today you have 0 sales..."), so the legs
    // assert the substance — figures present, reminder
    // present once and absent after — never the fixed
    // paper's exact phrasing.
    await say("How is today going?");
    got = await fresh();
    check(/sales|sale\(s\)/.test(got) && got.includes("you have not set a daily giveaway allowance"), "leg 3: the day answer carries the reminder the first time", got);

    await say("How is today going?");
    got = await fresh();
    check(/sales|sale\(s\)/.test(got) && !got.includes("you have not set"), "leg 4: the same day, the reminder does not ride again", got);

    const note = outbox().find((r) => (r.body || "").includes("you have not set"));
    check(!!note, "leg 5: the reminder's own message id is on record", "");
    await say("set it 300", note ? note.id : "");
    got = await fresh(60000);
    const shop = JSON.parse(fs.readFileSync(path.join(dir, "catalogue.json"), "utf8")).merchants.calm;
    check(got.includes("allowance is now") && shop.dailyGiveawayAllowance === 300 && shop.allowanceSet === true, "leg 5: answering the note sets the allowance in the book", got + " | book: " + JSON.stringify(shop));

    await say("Ok");
    got = await fresh(60000);
    check(got !== "" && !got.includes("sale(s)"), "leg 6: a bare ok is met with a nod", got);
  } finally {
    child.kill("SIGTERM");
  }
  console.log(failures === 0 ? "PROOF GREEN" : "PROOF RED: " + failures + " leg(s) failed");
  process.exit(failures === 0 ? 0 : 1);
})().catch((e) => { console.log("FAIL: " + e.message); process.exit(1); });
