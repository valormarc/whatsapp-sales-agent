// Live proof for returning-buyer recognition, with the real
// model: a buyer whose last paid order was delivered to
// Koforidua in size 40 comes back, is welcomed back, is
// offered her usual to confirm, and her size is remembered
// into the new order without being asked.
const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

const PORT = 3475;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "returning-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000777";
const BUYER = "233551117777";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    KOFI: { id: "KOFI", shopCode: "KOFI", name: "Kofi Shoes", ownerWhatsApp: OWNER,
      payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 25,
      deliveryNote: "Sent by bus to your station",
      products: [
        { id: "heels", name: "Ankle Strap Heels", description: "Pointed ankle-strap heels in mesh", listPrice: 120, floorPrice: 100, stock: 3, sizes: ["37", "38", "39", "40", "41", "42"] },
      ] },
  },
}));
const yesterday = new Date(Date.now() - 24 * 3600 * 1000).toISOString();
fs.writeFileSync(path.join(dir, "data", "orders.json"), JSON.stringify({
  "KOFI-0001": { reference: "KOFI-0001", from: BUYER, createdAt: yesterday, productName: "Ankle Strap Heels",
    price: 110, status: "paid", merchantId: "KOFI", fulfilment: "delivery", location: "Koforidua",
    packageName: "Efua Mensima", size: "40" },
}));
const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1" };
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
function orders() { const f = path.join(dir, "data", "orders.json"); return fs.existsSync(f) ? Object.values(JSON.parse(fs.readFileSync(f, "utf8"))) : []; }

(async () => {
  let pass = 0, fail = 0;
  const check = (name, ok) => { console.log((ok ? "PASS " : "FAIL ") + name); ok ? pass++ : fail++; };
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await new Promise((r) => setTimeout(r, 400)); }

    await msg(BUYER, "SHOP-KOFI");
    await waitFor(told(BUYER, "Welcome back to Kofi Shoes."), "welcomed back");
    check("she is welcomed back by name of the shop", true);
    await msg(BUYER, "Ankle Strap Heels");
    await waitFor(told(BUYER, "GH₵120"), "heels viewed");
    await msg(BUYER, "110");
    const before = outboxTo(BUYER).length;
    await waitFor(() => outboxTo(BUYER).length > before, "counter or deal");
    const all = outboxTo(BUYER).map((r) => r.body || "").join("\n");
    if (!all.includes("yours for")) {
      const figs = [...all.matchAll(/GH₵([\d,]+)/g)].map((m) => parseFloat(m[1].replace(",", "")));
      await msg(BUYER, String(figs[figs.length - 1]));
    }
    await waitFor(told(BUYER, "Last time you had it delivered to Koforidua. Same again, or different?"), "usual held up");
    check("last time's delivery is held up to confirm", true);
    await msg(BUYER, "Same please");
    await waitFor(told(BUYER, "What name should go on the package?"), "straight to details, size remembered");
    check("no size question: her last size is remembered", true);
    await msg(BUYER, "Efua Mensima");
    await waitFor(told(BUYER, "Any message for the seller"), "note asked");
    await msg(BUYER, "no");
    await waitFor(() => outboxTo(BUYER).some((r) => { const b = r.body || ""; return b.includes("40") && b.includes("Koforidua") && b.toLowerCase().includes("confirm"); }), "recap carries remembered size and place");
    check("the recap reads back her size and her place", true);
    await msg(BUYER, "yes");
    await waitFor(() => orders().length === 2, "second order recorded");
    const o = orders().find((x) => x.reference === "KOFI-0002");
    check("the new order carries her usual whole", !!o && o.fulfilment === "delivery" && o.location === "Koforidua" && o.size === "40");
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    agent.kill("SIGKILL");
    console.log(`\nreturning live proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
