// Live proof for the per-turn model budget, with the real
// model behind a counting relay: the scratch agent talks
// to a local relay, the relay forwards every call to the
// real Meta API and counts it, so each turn's model spend
// is measured, not assumed. The heavy turns are the ones
// that used to double-spend: an offer (proposal only), a
// discovery question (one read), a question inside
// fulfilment (a read plus its drafted answer), and a
// pasted advert that lists, judges and announces in one
// turn (a read, one composition judgement, one draft).
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

const REAL_KEY = process.env.META_MODEL_API_KEY;
if (!REAL_KEY) { console.log("META_MODEL_API_KEY required"); process.exit(1); }

let relayHits = 0;
const relay = http.createServer(async (req, res) => {
  relayHits++;
  let body = "";
  req.on("data", (c) => (body += c));
  req.on("end", async () => {
    try {
      const up = await fetch("https://api.meta.ai/v1/chat/completions", {
        method: "POST",
        headers: { "content-type": "application/json", authorization: "Bearer " + REAL_KEY },
        body,
      });
      res.writeHead(up.status, { "content-type": "application/json" });
      res.end(await up.text());
    } catch (e) {
      res.writeHead(502); res.end("{}");
    }
  });
});

const PORT = 3481;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "budget-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000888";
const BUYER = "233551118888";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    KOFI: { id: "KOFI", shopCode: "KOFI", name: "Kofi Shoes", ownerWhatsApp: OWNER,
      payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 25,
      deliveryNote: "Sent by bus to your station",
      products: [
        { id: "heels", name: "Ankle Strap Heels", description: "Pointed ankle-strap heels in mesh", listPrice: 120, floorPrice: 100, stock: 3 },
      ] },
  },
}));
let agent;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

let seq = 0;
async function msg(from, text) {
  seq++;
  await fetch(`${BASE}/webhook`, { method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ object: "whatsapp_business_account", entry: [{ changes: [{ value: { messages: [{ from, id: "w" + seq, type: "text", text: { body: text } }] } }] }] }) });
}
function outboxTo(who) {
  const f = path.join(dir, "data", "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)).filter((r) => r.to === who);
}
// Send one message and wait until its replies settle, then
// report how many model calls the turn spent.
async function turn(from, text) {
  const callsBefore = relayHits;
  const outBefore = outboxTo(from).length;
  await msg(from, text);
  let lastCount = -1, stable = 0;
  for (let i = 0; i < 60; i++) {
    await sleep(600);
    const n = outboxTo(from).length;
    if (n === lastCount) { stable++; } else { stable = 0; lastCount = n; }
    if (n > outBefore && stable >= 3) break;
    if (i > 40 && n === outBefore && stable >= 3) break;
  }
  await sleep(400);
  return relayHits - callsBefore;
}
function catalogue() { return JSON.parse(fs.readFileSync(path.join(dir, "data", "catalogue.json"), "utf8")); }

(async () => {
  let pass = 0, fail = 0;
  const check = (name, ok) => { console.log((ok ? "PASS " : "FAIL ") + name); ok ? pass++ : fail++; };
  try {
    await new Promise((r) => relay.listen(3599, "127.0.0.1", r));
    const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"),
      WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1", META_MODEL_API_KEY: "relay-dummy", META_MODEL_API_BASE: "http://127.0.0.1:3599/v1" };
    delete env.SEEV_SECRET_KEY; delete env.SEEV_API_KEY;
    agent = spawn(path.join(__dirname, "..", "agent"), [], { env, stdio: "ignore" });
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }

    let calls = await turn(BUYER, "Where can I buy heels?");
    check(`discovery question spends one call (spent ${calls})`, calls === 1);

    await turn(BUYER, "SHOP-KOFI");
    await turn(BUYER, "Ankle Strap Heels");
    calls = await turn(BUYER, "110");
    const all = outboxTo(BUYER).map((r) => r.body || "").join("\n");
    if (!all.includes("yours for")) {
      const figs = [...all.matchAll(/GH₵([\d,]+)/g)].map((m) => parseFloat(m[1].replace(",", "")));
      calls = await turn(BUYER, String(figs[figs.length - 1]));
    }
    check(`the offer turn spends one call, the proposal (spent ${calls})`, calls === 1);

    calls = await turn(BUYER, "How long will delivery take?");
    check(`a question inside fulfilment spends at most two (spent ${calls})`, calls >= 1 && calls <= 2);

    await turn(BUYER, "Delivery please, I am in Koforidua");
    await turn(BUYER, "Efua Mensima");
    await turn(BUYER, "no");
    await turn(BUYER, "yes");
    await turn(BUYER, "no more please");

    // The advert opens its listing walk; drive it to the
    // save, counting every turn. The save turn is the heavy
    // one: a walk read, the composition judgement and the
    // announcement draft.
    calls = await turn(OWNER, "Brand new leather handbags for sale\nPrice: GH₵850\nLast price GH₵800\nSizes: S, M, L\nDelivery available nationwide");
    check(`the advert turn spends at most three (spent ${calls})`, calls <= 3);
    let saveTurnCalls = 0;
    let bag = null;
    for (let i = 0; i < 7 && !bag; i++) {
      const lastQ = (outboxTo(OWNER).map((r) => r.body || "").pop() || "").toLowerCase();
      let answer = "yes";
      if (lastQ.includes("last price")) answer = "800";
      else if (lastQ.includes("allowance") || lastQ.includes("give away")) answer = "300";
      else if (lastQ.includes("deliver")) answer = "Delivery is GH₵25, sent by bus";
      else if (lastQ.includes("measure") || lastQ.includes("by the")) answer = "Sold as a whole piece";
      saveTurnCalls = await turn(OWNER, answer);
      const kofi = catalogue().merchants.KOFI;
      bag = (kofi.products || []).find((p) => p.listPrice === 850) || null;
    }
    check(`the save turn spends at most three (spent ${saveTurnCalls})`, !!bag && saveTurnCalls <= 3);
    check("the advert still lists, judged in the same turn", !!bag && (bag.sizes || []).length === 3);
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    if (agent) agent.kill("SIGKILL");
    relay.close();
    console.log(`\nbudget live proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
