// Live proof for the delivery fee system, with the real
// model key. Three shops' worth of behaviour in one run:
// a "set" shop charges the fee for the buyer's named area
// (not the standard fee) inside her total; a "quote" shop
// pauses the sale, the seller names the fee in her own
// chat, and the sale completes with that fee inside the
// total; and a seller who cannot deliver sends the buyer
// back to the choice, politely, with nothing paid.
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

const PORT = 3493;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "fees-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER1 = "233240000881", OWNER2 = "233240000882";
const B1 = "233551118881", B2 = "233551118882", B3 = "233551118883";
const heels = { id: "heels", name: "Ankle Strap Heels", description: "Pointed ankle-strap heels in mesh", listPrice: 120, floorPrice: 100, stock: 3 };
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    AREAS: { id: "AREAS", shopCode: "AREAS", name: "Area Shoes", ownerWhatsApp: OWNER1,
      payoutPhone: OWNER1, dailyGiveawayAllowance: 300, deliveryFee: 25, deliveryPaidTo: "merchant",
      deliveryAreas: [{ area: "Osu", fee: 30 }],
      products: [heels] },
    QUOTE: { id: "QUOTE", shopCode: "QUOTE", name: "Quote Shoes", ownerWhatsApp: OWNER2,
      payoutPhone: OWNER2, dailyGiveawayAllowance: 300, deliveryStyle: "quote", deliveryPaidTo: "merchant",
      products: [heels] },
  },
}));
const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1" };
delete env.SEEV_SECRET_KEY; delete env.SEEV_API_KEY;
const agent = spawn(path.join(__dirname, "..", "agent"), [], { env, stdio: "ignore" });

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
const bodies = (rs) => rs.map((r) => r.body || "").join("\n");
function orders() {
  const f = path.join(dir, "data", "orders.json");
  if (!fs.existsSync(f)) return [];
  return Object.values(JSON.parse(fs.readFileSync(f, "utf8")));
}
async function buyToDelivery(buyer, code, place) {
  await say(buyer, "SHOP-" + code);
  await say(buyer, "Ankle Strap Heels");
  await say(buyer, "120");
  return say(buyer, "Delivery to " + place);
}
async function finishDetails(buyer) {
  await say(buyer, "Akosua Mensah");
  await say(buyer, "no");
  return say(buyer, "yes");
}

(async () => {
  let pass = 0, fail = 0;
  const check = (name, ok) => { console.log((ok ? "PASS " : "FAIL ") + name); ok ? pass++ : fail++; };
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }

    // 1. Set shop, named area: Osu's own fee, not the standard.
    await buyToDelivery(B1, "AREAS", "Osu");
    const done1 = await finishDetails(B1);
    check("the area fee is inside the total she confirms", bodies(done1).includes("GH₵150") || bodies(outboxTo(B1)).includes("GH₵150"));
    const o1 = orders().find((o) => o.reference.startsWith("AREAS"));
    check("the order carries the area fee of 30, not the standard 25", !!o1 && o1.deliveryFee === 30 && o1.total === 150);

    // 2. Quote shop: pause, ask, answer, complete.
    const asked = await buyToDelivery(B2, "QUOTE", "Kasoa");
    check("the buyer is told the fee is being asked", bodies(asked).includes("I have asked her"));
    check("the seller is asked in her own chat", bodies(outboxTo(OWNER2)).includes("Delivery fee needed") && bodies(outboxTo(OWNER2)).includes("Kasoa"));
    const told = await say(OWNER2, "fee 35");
    check("the buyer hears the quoted fee", bodies(outboxTo(B2)).includes("says delivery to Kasoa is GH₵35"));
    const done2 = await finishDetails(B2);
    check("the quoted fee is inside the total she pays", bodies(done2).includes("GH₵155") || bodies(outboxTo(B2)).includes("GH₵155"));
    const o2 = orders().find((o) => o.reference.startsWith("QUOTE"));
    check("the order carries the quoted fee, locked at her confirmation", !!o2 && o2.deliveryFee === 35 && o2.total === 155);

    // 3. Quote shop, refusal: the choice comes back.
    await buyToDelivery(B3, "QUOTE", "Tema");
    await say(OWNER2, "no delivery");
    const back = bodies(outboxTo(B3));
    check("a refusal tells the buyer plainly", back.includes("cannot deliver to Tema"));
    check("no order was made from the refused delivery", orders().filter((o) => o.reference.startsWith("QUOTE")).length === 1);
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    agent.kill("SIGKILL");
    console.log(`\ndelivery fees live proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
