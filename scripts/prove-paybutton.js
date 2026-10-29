// Live proof for the tap-to-pay button, with the real
// model key: a completed sale sends the payment link as a
// WhatsApp button carrying the total on its face, and no
// raw link reaches the buyer as chat text anywhere in the
// whole conversation.
const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

const PORT = 3485;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "paybtn-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000111";
const BUYER = "233551110111";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    KOFI: { id: "KOFI", shopCode: "KOFI", name: "Kofi Shoes", ownerWhatsApp: OWNER,
      payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 25,
      deliveryNote: "Sent by bus to your station", deliveryPaidTo: "merchant",
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
    const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1" };
    delete env.SEEV_SECRET_KEY; delete env.SEEV_API_KEY;
    agent = spawn(path.join(__dirname, "..", "agent"), [], { env, stdio: "ignore" });
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }

    await say(BUYER, "SHOP-KOFI");
    await say(BUYER, "Ankle Strap Heels");
    await say(BUYER, "110");
    const haggle = bodies(outboxTo(BUYER)).join("\n");
    if (!haggle.includes("yours for")) {
      const figs = [...haggle.matchAll(/GH₵([\d,]+)/g)].map((m) => parseFloat(m[1].replace(",", "")));
      await say(BUYER, String(figs[figs.length - 1]));
    }
    await say(BUYER, "Delivery to Osu please");
    for (let i = 0; i < 6; i++) {
      const lastQ = (bodies(outboxTo(BUYER)).pop() || "").toLowerCase();
      let answer = "yes";
      if (lastQ.includes("where should it go") || lastQ.includes("where are you")) answer = "Osu";
      else if (lastQ.includes("what name should go")) answer = "Efua Mensima";
      else if (lastQ.includes("any message for the seller")) answer = "no";
      await say(BUYER, answer);
      if (outboxTo(BUYER).some((r) => r.kind === "cta_url")) break;
    }

    const all = outboxTo(BUYER);
    const cta = all.find((r) => r.kind === "cta_url");
    check("the payment arrives as a tap-to-pay button", !!cta);
    if (cta) {
      const total = (bodies(all).join("\n").match(/Total: GH₵([\d,]+)/) || [])[1];
      check("the amount she pays is on the button's face", !!total && (cta.buttons || [])[0] === `Click to pay GH₵${total}`);
      check("the button leads to the payment page", typeof cta.url === "string" && cta.url.startsWith("http"));
      check("the button's body is the plain instruction", cta.body === "Pay here to complete your order.");
    }
    const rawLink = all.filter((r) => r.kind !== "cta_url" && /https?:\/\//.test(r.body || ""));
    check("no raw link reaches her as chat text", rawLink.length === 0);
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    if (agent) agent.kill("SIGKILL");
    console.log(`\npay button live proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
