// Live proof for sizes, with the real model:
// 1. A seller lists heels with an advert that states a size
//    range; the saved product must carry each size.
// 2. A buyer buys them: the details ask her size, the recap
//    reads it back, and the order carries it.
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

const PORT = 3467;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "sizes-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000777";
const BUYER = "233551117777";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    KOFI: { id: "KOFI", shopCode: "KOFI", name: "Kofi Shoes", ownerWhatsApp: OWNER,
      payoutPhone: OWNER, dailyGiveawayAllowance: 300, products: [] },
  },
}));
const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1" };
delete env.SEEV_SECRET_KEY; delete env.SEEV_API_KEY;
const agent = spawn(path.join(__dirname, "..", "agent"), [], { env, stdio: "ignore" });

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
async function waitFor(fn, label, timeoutMs = 60000) {
  const start = Date.now();
  for (;;) {
    if (fn()) return true;
    if (Date.now() - start > timeoutMs) throw new Error("timeout: " + label);
    await new Promise((r) => setTimeout(r, 700));
  }
}
const told = (who, sub) => () => outboxTo(who).some((r) => (r.body || "").toLowerCase().includes(sub.toLowerCase()));
function catalogue() { return JSON.parse(fs.readFileSync(path.join(dir, "data", "catalogue.json"), "utf8")); }
function orders() { const f = path.join(dir, "data", "orders.json"); return fs.existsSync(f) ? Object.values(JSON.parse(fs.readFileSync(f, "utf8"))) : []; }

(async () => {
  let pass = 0, fail = 0;
  const check = (name, ok) => { console.log((ok ? "PASS " : "FAIL ") + name); ok ? pass++ : fail++; };
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await new Promise((r) => setTimeout(r, 400)); }

    // 1. The advert listing. Drive the walk by what it asks,
    // waiting for each new reply before answering it.
    await msg(OWNER, "Ankle Strap Heels\nVery fine quality, sizes 37 to 42 available\nPrice GH₵120");
    let answeredUpTo = 0;
    for (let turn = 0; turn < 10; turn++) {
      if ((catalogue().merchants.KOFI.products || []).length > 0) break;
      await waitFor(() => outboxTo(OWNER).length > answeredUpTo, "walk reply " + turn, 40000);
      const rows = outboxTo(OWNER);
      answeredUpTo = rows.length;
      const last = (rows[rows.length - 1].body || "").toLowerCase();
      if (last.includes("last price")) await msg(OWNER, "100");
      else if (last.includes("go live") || last.includes("shall i") || last.includes("save it")) await msg(OWNER, "yes");
      else if (last.includes("price") && last.includes("?")) await msg(OWNER, "yes");
      else if (last.includes("called") || last.includes("name")) await msg(OWNER, "Ankle Strap Heels");
      else if (last.includes("describe") || last.includes("description")) await msg(OWNER, "Pointed ankle-strap heels in mesh");
      else await msg(OWNER, "yes");
    }
    await waitFor(() => (catalogue().merchants.KOFI.products || []).length > 0, "product saved");
    const prod = catalogue().merchants.KOFI.products[0];
    check("the advert's size range became the listing's sizes", JSON.stringify(prod.sizes) === JSON.stringify(["37", "38", "39", "40", "41", "42"]));

    // 2. Buy them and pick a size.
    await msg(BUYER, "SHOP-KOFI");
    await waitFor(told(BUYER, "Ankle Strap Heels"), "buyer welcomed with the shelf");
    await msg(BUYER, "Ankle Strap Heels");
    await waitFor(told(BUYER, "GH₵120"), "heels viewed");
    await msg(BUYER, "I will pay 120");
    await waitFor(told(BUYER, "Where are you based?"), "fulfilment opened");
    await msg(BUYER, "Accra");
    await waitFor(told(BUYER, "What size do you take?"), "size asked");
    check("the buyer is asked her size before anything else in the details", true);
    await msg(BUYER, "41");
    await waitFor(told(BUYER, "What name should go on the package?"), "name asked after size");
    await msg(BUYER, "Efua Mensima");
    await waitFor(told(BUYER, "Any message for the seller"), "note asked");
    await msg(BUYER, "no");
    // The recap is the model-drafted read-back: it must carry
    // her size in its own words, not necessarily as a label.
    await waitFor(() => outboxTo(BUYER).some((r) => { const b = (r.body || "").toLowerCase(); return b.includes("confirm") && b.includes("41"); }), "recap reads the size back");
    const recap = outboxTo(BUYER).map((r) => r.body || "").filter((b) => b.toLowerCase().includes("confirm")).pop();
    console.log("recap seen: " + JSON.stringify(recap));
    check("the recap reads her size back", true);
    await msg(BUYER, "yes");
    await waitFor(() => orders().length === 1, "order recorded");
    check("the order carries her size", orders()[0].size === "41");
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    agent.kill("SIGKILL");
    console.log(`\nsizes live proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
