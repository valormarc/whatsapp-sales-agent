// Live proof for Clean Records, with the real model key:
// the money messages arrive on fixed paper. A measured
// product's quote is the demo's own shape, the completion
// total is one message in the demo's lines, the seller's
// notice is one message in the fixed order, and a confirmed
// payment hands the buyer the payment record. Talk around
// the records stays in pieces, as before.
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

const PORT = 3483;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "records-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000999";
const BUYER = "233551119999";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    EFUA: { id: "EFUA", shopCode: "EFUA", name: "Efua's Store", ownerWhatsApp: OWNER,
      payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 25,
      deliveryNote: "Sent by bus to your station", deliveryPaidTo: "merchant",
      products: [
        { id: "ankara", name: "Ankara fabric", description: "Bright Ankara print cloth", listPrice: 450, floorPrice: 380, stock: 5, unit: "yard", baseQty: 6 },
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
const bodies = (rs) => rs.map((r) => r.body || "");

(async () => {
  let pass = 0, fail = 0;
  const check = (name, ok) => { console.log((ok ? "PASS " : "FAIL ") + name); ok ? pass++ : fail++; };
  try {
    const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1" };
    delete env.SEEV_SECRET_KEY; delete env.SEEV_API_KEY;
    agent = spawn(path.join(__dirname, "..", "agent"), [], { env, stdio: "ignore" });
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }

    await say(BUYER, "SHOP-EFUA");
    const view = bodies(await say(BUYER, "Ankara fabric"));
    check("the quote is one message on the demo paper", view.includes("Ankara fabric, 6 yards\nPrice: GH₵450"));

    await say(BUYER, "400");
    const haggle = bodies(outboxTo(BUYER)).join("\n");
    const figs = [...haggle.matchAll(/GH₵([\d,]+)/g)].map((m) => parseFloat(m[1].replace(",", "")));
    const counter = figs[figs.length - 1];
    if (counter && counter !== 400) await say(BUYER, String(counter));
    await say(BUYER, "Delivery to Osu please");
    // Answer the question actually asked, one step at a
    // time, until the sale completes: the place, the name,
    // the note, the confirmation.
    let done = [];
    for (let i = 0; i < 6; i++) {
      const lastQ = (bodies(outboxTo(BUYER)).pop() || "").toLowerCase();
      let answer = "yes";
      if (lastQ.includes("where should it go") || lastQ.includes("where are you")) answer = "Osu";
      else if (lastQ.includes("what name should go")) answer = "Efua Mensima";
      else if (lastQ.includes("any message for the seller")) answer = "no";
      done = bodies(await say(BUYER, answer));
      if (done.some((b) => b.includes("Pay here to complete"))) break;
    }
    const totalRec = done.find((b) => b.startsWith("Ankara fabric: GH₵"));
    check("the total is one record message", !!totalRec && totalRec.includes("\nDelivery to Osu: GH₵25\nTotal: GH₵"));
    const agreed = totalRec ? parseFloat((totalRec.match(/Total: GH₵([\d,]+)/) || [])[1].replace(",", "")) : 0;
    check("the total adds up", agreed > 380 && agreed <= 450 + 25);

    const notes = bodies(outboxTo(OWNER));
    const notice = notes.find((b) => b.startsWith("New order EFUA-"));
    check("the seller's notice is one message", !!notice);
    if (notice) {
      const order = ["New order EFUA-", "Item:", "Agreed price: GH₵", "Delivery:", "Total: GH₵", "Payment: waiting", "Today:"];
      let at = -1, ordered = true;
      for (const w of order) { const i = notice.indexOf(w); if (i < 0 || i < at) ordered = false; at = i; }
      check("the notice lines stand in the fixed order", ordered);
    }

    await say(BUYER, "I have paid");
    await say(OWNER, "yes");
    const buyerAll = bodies(outboxTo(BUYER));
    const confirmed = buyerAll.find((b) => b.startsWith("Payment confirmed\nItem:"));
    check("payment confirmed arrives as the record", !!confirmed && confirmed.includes("\nAmount: GH₵") && confirmed.includes("\nOrder: EFUA-") && confirmed.includes("Thank you for buying from Efua's Store."));
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    if (agent) agent.kill("SIGKILL");
    console.log(`\nrecords live proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
