// Reasoning eval: a fixed battery of real-world phrasings
// run against the LIVE model before a deploy is called
// smart. It boots ./agent on a scratch port with a seeded
// catalogue and the real META_MODEL_API_KEY from the
// environment, plays buyers and a seller the way people
// actually talk (references, pronouns, questions inside
// steps, call-offs with no trigger words), and scores the
// replies. Stub tests prove logic; this proves reasoning.
//
//   META_MODEL_API_KEY=... node scripts/eval-reasoning.js
//
// Exit code is the number of failed checks (0 = all pass).
const { spawn } = require("child_process");
const fs = require("fs");
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

const PORT = 3211;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "eval-reasoning-"));
fs.writeFileSync(path.join(dir, "catalogue.json"), JSON.stringify({ merchants: {
  luxe: { id: "luxe", name: "LUXE", shopCode: "LUXE", ownerWhatsApp: "233200000001", payoutPhone: "233200000001", dailyGiveawayAllowance: 100, products: [
    { id: "heels", name: "Ankle Strap Heels", description: "Black ankle strap heels, size 40.", listPrice: 120, floorPrice: 100, stock: 2 } ] },
  efua: { id: "efua", name: "Efua's Store", shopCode: "EFUA", ownerWhatsApp: "233200000002", payoutPhone: "233200000002", dailyGiveawayAllowance: 300, deliveryFee: 25, deliveryNote: "Sent by bus to your station.", deliveryPaidTo: "merchant", products: [
    { id: "beaded-bag", name: "Handmade Beaded Bag", description: "Hand-beaded evening bag with a gold clasp.", listPrice: 850, floorPrice: 680, stock: 3 },
    { id: "shea", name: "Raw Shea Butter (500g)", description: "Unrefined shea butter from Tamale.", listPrice: 120, floorPrice: 95, stock: 40 } ] },
} }));

const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: dir, WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1" };
const agent = spawn("./agent", [], { env, stdio: "ignore" });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let cursor = 0;
function fresh() {
  const f = path.join(dir, "outbox.jsonl");
  const lines = fs.existsSync(f) ? fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean) : [];
  const out = lines.slice(cursor).map((l) => { try { return JSON.parse(l); } catch { return {}; } });
  cursor = lines.length;
  return out;
}
async function say(from, text) {
  const payload = { entry: [{ changes: [{ value: { messages: [{ from, type: "text", text: { body: text } }] } }] }] };
  await fetch(`http://localhost:${PORT}/webhook`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(payload) });
  // Poll until this turn's replies have actually landed and
  // settled: a fixed sleep misreads slow turns, crediting
  // one turn's replies to the next.
  const got = [];
  let idle = 0;
  for (let i = 0; i < 30 && idle < 2; i++) {
    await sleep(1500);
    const batch = fresh().filter((m) => m.to === from).map((m) => m.body || "");
    if (batch.length) { got.push(...batch); idle = 0; } else if (got.length) { idle++; }
  }
  return got;
}
let pass = 0, fail = 0;
function check(name, cond, detail) {
  if (cond) { pass++; console.log("PASS", name); }
  else { fail++; console.log("FAIL", name, "—", detail); }
}

(async () => {
  await sleep(1200);
  // 1. A picture ask that refers back to shown results by description.
  let b = "233200005001";
  await say(b, "I'm looking for heels");
  let r = await say(b, "the strappy ones, can I see a picture?");
  check("discovery referent picture", r.join("\n").includes("don't have the pic of the Ankle Strap Heels"), r.join(" | "));

  // 2. A comparative reference: "the cheaper one".
  b = "233200005002";
  await say(b, "SHOP-EFUA");
  await say(b, "Handmade Beaded Bag");
  await say(b, "Raw Shea Butter");
  r = await say(b, "the cheaper one, how much is it?");
  check("comparative referent", r.join("\n").includes("GH₵120") && /shea/i.test(r.join("\n")), r.join(" | "));

  // 3 + 4. Mid-sale: picture by meaning, then a question about the total.
  b = "233200005003";
  await say(b, "SHOP-EFUA");
  await say(b, "Handmade Beaded Bag");
  await say(b, "850");
  r = await say(b, "Before I pay I need to see it");
  check("mid-sale picture by meaning", r.join("\n").includes("don't have the pic of the Handmade Beaded Bag"), r.join(" | "));
  r = await say(b, "And that price, does it include the delivery?");
  const joined = r.join("\n");
  check("stage question answered", joined.includes("850") && joined.includes("25") && /how do you want to get it/i.test(joined), joined);

  // 5. Seller side: a question inside her own listing walk.
  const owner = "233200000002";
  await say(owner, "add a product");
  await say(owner, "Kente Scarf");
  await say(owner, "Handwoven kente scarf, bright colours.");
  await say(owner, "300");
  r = await say(owner, "Sorry, what do you mean by last price?");
  check("seller walk question answered", /lowest/i.test(r.join("\n")) && /last price/i.test(r.join("\n")), r.join(" | "));
  r = await say(owner, "250");
  check("seller walk continues", /measure|Shall I add/i.test(r.join("\n")), r.join(" | "));

  // 6. A call-off with no trigger word cancels the sale.
  b = "233200005006";
  await say(b, "SHOP-EFUA");
  await say(b, "Handmade Beaded Bag");
  await say(b, "850");
  r = await say(b, "Actually, drop the whole thing");
  check("cancel by meaning", r.join("\n").includes("Okay, cancelled."), r.join(" | "));

  console.log(`\nreasoning eval: ${pass} pass, ${fail} fail`);
  agent.kill();
  process.exit(fail);
})().catch((e) => { console.error(e); agent.kill(); process.exit(99); });
