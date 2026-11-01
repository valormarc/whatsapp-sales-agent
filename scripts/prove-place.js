#!/usr/bin/env node
// Live proof for the place inside an answer: a returning
// buyer whose last order went to Koforidua is asked "Same
// again, or different?" and answers "Yes accra" —
// agreement and a new place in one breath. The place that
// lands on her recap and on the order must be accra alone.
// A record that stored the whole reply shipped two real
// orders to a town called Yes. Real model, dry-run
// WhatsApp. Run from the repo root with the agent binary
// built.
const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");
const net = require("net");

delete process.env.WHATSAPP_TOKEN;
delete process.env.WHATSAPP_PHONE_NUMBER_ID;
delete process.env.WHATSAPP_APP_SECRET;
delete process.env.WHATSAPP_VERIFY_TOKEN;
delete process.env.SEEV_SECRET_KEY;
delete process.env.SEEV_API_KEY;
delete process.env.SEEV_WEBHOOK_SECRET;

const ROOT = path.join(__dirname, "..");
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "place-"));
const OWNER = "233240000701";
const BUYER = "233240000711";
fs.writeFileSync(path.join(dir, "catalogue.json"), JSON.stringify({
  merchants: {
    efua: { id: "efua", name: "Efua's Store", shopCode: "EFUA", ownerWhatsApp: OWNER, payoutPhone: OWNER,
      dailyGiveawayAllowance: 300, deliveryFee: 20, deliveryPaidTo: "merchant",
      products: [{ id: "bag", name: "Handmade Beaded Bag", description: "Hand-beaded evening bag with a gold clasp.", listPrice: 850, floorPrice: 680, stock: 3 }] },
  },
}));
const prev = { reference: "EFUA-0001", status: "paid", from: BUYER, merchantId: "efua", productName: "Handmade Beaded Bag", price: 850, packageName: "Efua Mensima", fulfilment: "delivery", location: "Koforidua", createdAt: "2026-10-30T10:00:00Z" };
fs.writeFileSync(path.join(dir, "orders.json"), JSON.stringify({ "EFUA-0001": prev }));

const freePort = () => new Promise((res, rej) => { const s = net.createServer(); s.once("error", rej); s.listen(0, () => { const p = s.address().port; s.close(() => res(p)); }); });
let seq = 0, passed = 0, failed = 0;
const seen = new Map();
function outbox(to) {
  const f = path.join(dir, "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").split("\n").filter(Boolean).map((l) => { try { return JSON.parse(l); } catch { return null; } }).filter((r) => r && r.to === to);
}
function fresh(to) { const all = outbox(to); const n = seen.get(to) || 0; seen.set(to, all.length); return all.slice(n); }
const body = (rs) => rs.map((r) => r.body || "").join("\n");
async function waitFor(to, pred, what, timeout = 150000) {
  const start = Date.now(); let acc = [];
  while (Date.now() - start < timeout) {
    acc = acc.concat(fresh(to));
    if (pred(body(acc), acc)) return acc;
    await new Promise((r) => setTimeout(r, 1500));
  }
  throw new Error("timeout waiting for " + what + "; got: " + body(acc).slice(0, 400));
}
function check(name, ok, extra = "") {
  if (ok) { passed++; console.log("  ok  " + name); }
  else { failed++; console.log("FAIL  " + name + (extra ? "  [" + String(extra).slice(0, 220) + "]" : "")); }
}
function orders() { const f = path.join(dir, "orders.json"); return fs.existsSync(f) ? Object.values(JSON.parse(fs.readFileSync(f, "utf8"))) : []; }

(async () => {
  const port = await freePort();
  const base = "http://127.0.0.1:" + port;
  const env = { ...process.env, WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1", HACKATHON_AGENT_DATA_DIR: dir, PORT: String(port) };
  const agent = spawn(path.join(ROOT, "agent"), [], { cwd: dir, env, stdio: ["ignore", "pipe", "pipe"] });
  let agentLog = "";
  agent.stdout.on("data", (d) => { agentLog += d; });
  agent.stderr.on("data", (d) => { agentLog += d; });
  const post = async (from, text) => {
    const msg = { from, id: "w" + ++seq, timestamp: String(Math.floor(Date.now() / 1000)), type: "text", text: { body: text } };
    const res = await fetch(base + "/webhook", { method: "POST", headers: { "content-type": "application/json" },
      body: JSON.stringify({ entry: [{ changes: [{ value: { messages: [msg] } }] }] }) });
    await res.text();
  };
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(base + "/healthz"); if (r.ok) break; } catch {} await new Promise((r) => setTimeout(r, 500)); }

    await post(BUYER, "SHOP-EFUA");
    await waitFor(BUYER, (b) => /welcome/i.test(b), "the shop welcome");
    await post(BUYER, "Handmade Beaded Bag");
    await waitFor(BUYER, (b) => b.includes("850"), "the bag at its price");
    await post(BUYER, "850");
    const usual = await waitFor(BUYER, (b) => b.includes("Same again, or different?"), "the usual question");
    check("her usual is held up with the old place", body(usual).includes("Koforidua"), body(usual));

    await post(BUYER, "Yes accra");
    await waitFor(BUYER, (b) => /name should go on the package/i.test(b), "the package name question");
    await post(BUYER, "Efua Mensima");
    await waitFor(BUYER, (b) => /message for the seller|anything.*seller/i.test(b), "the note question");
    await post(BUYER, "no");
    const recap = await waitFor(BUYER, (b) => /confirm your order/i.test(b), "the recap");
    const recapText = body(recap);
    check("the recap carries the place alone", recapText.includes("Delivery to accra"), recapText);
    check("no town called Yes anywhere in the recap", !/Yes accra/.test(recapText), recapText);

    await post(BUYER, "yes");
    await waitFor(BUYER, (b) => /pay/i.test(b), "the payment step");
    const mine = orders().filter((o) => o.from === BUYER && o.reference !== "EFUA-0001");
    check("the order was minted", mine.length === 1, JSON.stringify(mine).slice(0, 200));
    if (mine.length === 1) {
      check("the order's place is accra alone", mine[0].location === "accra", mine[0].location);
    }
  } catch (e) {
    failed++;
    console.log("FAIL  " + e.message);
    console.log(agentLog.slice(-800));
  } finally {
    agent.kill("SIGKILL");
  }
  console.log(failed === 0 ? "PROOF GREEN " + passed + "/" + (passed + failed) : "PROOF RED " + passed + "/" + (passed + failed));
  process.exit(failed === 0 ? 0 : 1);
})();
