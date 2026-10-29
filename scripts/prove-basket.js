// Live proof for the basket, with the real model: a buyer
// settles the heels, adds the bag at the fulfilment question
// (the thinking must read the add and name the item), settles
// the bag too, and completes ONE order carrying both items,
// one size, one total.
const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

const PORT = 3471;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "basket-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000888";
const BUYER = "233551118888";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    KOFI: { id: "KOFI", shopCode: "KOFI", name: "Kofi Shoes", ownerWhatsApp: OWNER,
      payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 25,
      deliveryNote: "Sent by bus to your station",
      products: [
        { id: "heels", name: "Ankle Strap Heels", description: "Pointed ankle-strap heels in mesh", listPrice: 120, floorPrice: 100, stock: 3, sizes: ["37", "38", "39", "40", "41", "42"] },
        { id: "bag", name: "Leather Bag", description: "A structured leather handbag", listPrice: 850, floorPrice: 800, stock: 2 },
      ] },
  },
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
    await waitFor(told(BUYER, "Ankle Strap Heels"), "shelf shown");
    await msg(BUYER, "Ankle Strap Heels");
    await waitFor(told(BUYER, "GH₵120"), "heels viewed");
    await msg(BUYER, "110");
    // Meet whatever counter comes back.
    await waitFor(() => outboxTo(BUYER).length >= 4, "counter or deal");
    const all = outboxTo(BUYER).map((r) => r.body || "").join("\n");
    if (!all.includes("yours for")) {
      const figs = [...all.matchAll(/GH₵([\d,]+)/g)].map((m) => parseFloat(m[1].replace(",", "")));
      const counter = figs[figs.length - 1];
      await msg(BUYER, String(counter));
    }
    await waitFor(told(BUYER, "How do you want to get it?"), "fulfilment opened");

    await msg(BUYER, "Add the Leather Bag too");
    await waitFor(told(BUYER, "Add the Leather Bag at GH₵850"), "add confirmed with her first");
    check("the add is confirmed before anything is parked", true);
    await msg(BUYER, "yes");
    await waitFor(told(BUYER, "Added."), "heels parked");
    await msg(BUYER, "850");
    await waitFor(told(BUYER, "That makes your order"), "running total on the second deal");
    check("the second deal carries the order's running total", true);

    await msg(BUYER, "Send it to Koforidua");
    await waitFor(told(BUYER, "What size for the Ankle Strap Heels?"), "size asked for the heels by name");
    await msg(BUYER, "40");
    await waitFor(told(BUYER, "What name should go on the package?"), "name asked");
    await msg(BUYER, "Efua Mensima");
    await waitFor(told(BUYER, "Any message for the seller"), "note asked");
    await msg(BUYER, "no");
    await waitFor(() => outboxTo(BUYER).some((r) => { const b = (r.body || ""); return b.includes("Leather Bag") && b.includes("Ankle Strap Heels") && b.toLowerCase().includes("confirm"); }), "recap names both items");
    check("the recap names both items", true);
    await msg(BUYER, "yes");
    await waitFor(() => orders().length === 1, "one order recorded");
    const o = orders()[0];
    check("one order carries both items", Array.isArray(o.items) && o.items.length === 2);
    check("the items sum to the order price", o.items && Math.abs(o.items.reduce((a, i) => a + i.price, 0) - o.price) < 0.001);
    check("the heels carry her size on the order", o.items && o.items[0].size === "40");
    const notes = outboxTo(OWNER).map((r) => r.body || "").join("\n");
    check("the seller's notice lists both items", notes.includes("*Ankle Strap Heels*") && notes.includes("*Leather Bag*"));
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    agent.kill("SIGKILL");
    console.log(`\nbasket live proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
