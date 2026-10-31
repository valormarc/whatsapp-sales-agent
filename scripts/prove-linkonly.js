// Live proof for link-only payment, with the real model
// key: a completed sale offers exactly one way to pay (the
// tap-to-pay button), the shop's payout number never reaches
// the buyer anywhere, asking for the number is refused with
// the button sent again, a report that money went elsewhere
// is answered as unconfirmable, a screenshot confirms
// nothing and is forwarded to no one, and no merchant is
// ever asked to confirm a payment. The order stays open
// through all of it: only a payment on the button settles.
// (This harness runs on the placeholder payment rail like
// the other proofs — no Seev keys — so a link report is
// answered with the honest "no session to check"; the
// Seev-checked road is covered by the Go tests against a
// fake read-back and was proven on the real sandbox rail
// during the verify-payment build.)
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

if (!process.env.META_MODEL_API_KEY) { console.log("META_MODEL_API_KEY required"); process.exit(1); }

const PORT = 3491;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "linkonly-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000777";
const BUYER = "233551117777";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    KOFI: { id: "KOFI", shopCode: "KOFI", name: "Kofi Shoes", ownerWhatsApp: OWNER,
      payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 25,
      deliveryNote: "Sent by bus to your station", deliveryPaidTo: "merchant",
      products: [
        { id: "heels", name: "Ankle Strap Heels", description: "Pointed ankle-strap heels in mesh", listPrice: 120, floorPrice: 100, stock: 3, sizes: ["37", "38", "39", "40"] },
      ] },
  },
}));
const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1" };
delete env.SEEV_SECRET_KEY; delete env.SEEV_API_KEY;
const agent = spawn(path.join(__dirname, "..", "agent"), [], { env, stdio: "ignore" });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let seq = 0;
async function post(message) {
  await fetch(`${BASE}/webhook`, { method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ object: "whatsapp_business_account", entry: [{ changes: [{ value: { messages: [message] } }] }] }) });
}
async function msg(from, text) { seq++; await post({ from, id: "w" + seq, type: "text", text: { body: text } }); }
async function img(from, media) { seq++; await post({ from, id: "w" + seq, type: "image", image: { id: media } }); }
function outboxTo(who) {
  const f = path.join(dir, "data", "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)).filter((r) => r.to === who);
}
async function say(from, text) {
  const before = outboxTo(from).length;
  await msg(from, text);
  let lastCount = -1, stable = 0;
  for (let i = 0; i < 50; i++) {
    await sleep(600);
    const n = outboxTo(from).length;
    if (n === lastCount) { stable++; } else { stable = 0; lastCount = n; }
    if (n > before && stable >= 4) break;
    if (i > 40 && n === before && stable >= 4) break;
  }
  await sleep(400);
  return outboxTo(from).slice(before);
}
async function sendImg(from, media) {
  const before = outboxTo(from).length;
  await img(from, media);
  for (let i = 0; i < 40; i++) { await sleep(600); if (outboxTo(from).length > before) break; }
  await sleep(400);
  return outboxTo(from).slice(before);
}
const bodies = (rs) => rs.map((r) => r.body || "").join("\n");
function orders() {
  const f = path.join(dir, "data", "orders.json");
  if (!fs.existsSync(f)) return [];
  return Object.values(JSON.parse(fs.readFileSync(f, "utf8")));
}

(async () => {
  let pass = 0, fail = 0;
  const check = (name, ok) => { console.log((ok ? "PASS " : "FAIL ") + name); ok ? pass++ : fail++; };
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }

    // A straight sale at list, out the pickup door.
    await say(BUYER, "SHOP-KOFI");
    await say(BUYER, "Ankle Strap Heels");
    await say(BUYER, "120");
    await say(BUYER, "I will pick it up myself");
    await say(BUYER, "40");
    await say(BUYER, "Akosua Mensah");
    await say(BUYER, "no");
    const done = await say(BUYER, "yes");
    check("the sale completes with the pay button", done.some((r) => r.kind === "cta_url"));
    const allBuyer = outboxTo(BUYER);
    check("the payout number never reaches the buyer", !allBuyer.some((r) => (r.body || "").includes(OWNER)));
    check("no direct-payment offer is made anywhere", !/send MoMo directly|MoMo directly to/i.test(bodies(allBuyer)));
    const ownerAfterSale = outboxTo(OWNER).length;

    // She asks for the number instead.
    const ask = await say(BUYER, "I will pay to the number");
    check("asking for the number is refused plainly", bodies(ask).includes("only through the payment button"));
    check("the button is sent again", ask.some((r) => r.kind === "cta_url"));
    check("the number is still not given", !bodies(ask).includes(OWNER));

    // She says the money went to a number anyway.
    const direct = await say(BUYER, "I have sent it to your number");
    check("a payment elsewhere is answered as unconfirmable", bodies(direct).includes("cannot check it or confirm it"));
    check("the merchant hears nothing about it", outboxTo(OWNER).length === ownerAfterSale);

    // She sends the screenshot.
    const shot = await sendImg(BUYER, "MEDIA-LINKONLY-1");
    check("a screenshot confirms nothing, said plainly", bodies(shot).includes("cannot confirm a payment here"));
    check("the screenshot is forwarded to no one", !outboxTo(OWNER).some((r) => r.kind === "image"));
    check("the merchant is still untroubled", outboxTo(OWNER).length === ownerAfterSale);

    // She says she has paid, full stop: the agent answers
    // from its own check road, and asks no one to confirm.
    const rep = await say(BUYER, "I have paid");
    check("her word is answered by the check road, not a claim", bodies(rep).includes("no payment session to check") || bodies(rep).includes("has not come through yet") || bodies(rep).includes("could not reach the payment service"));
    check("no merchant is asked to confirm anything", !/Reply yes if you have received it/i.test(bodies(outboxTo(OWNER))));

    const os_ = orders();
    check("the order is still open after all of it", os_.length === 1 && os_[0].status === "pending");
    check("no payment path was settled", os_.length === 1 && !os_[0].paymentPath);
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    agent.kill("SIGKILL");
    console.log(`\nlink-only live proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
