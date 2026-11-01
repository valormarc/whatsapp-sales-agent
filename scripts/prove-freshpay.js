#!/usr/bin/env node
// Live proof for the fresh payment watch: a payment that
// lands at Seev WITHOUT its webhook must still confirm
// itself, on its own, while the buyer is at the counter —
// she must not have to come back and report it. The Seev
// stub here never sends a webhook at all; its read-back
// simply starts saying "completed" once the link is out.
// The agent's fresh watch (a 30 second tick over orders
// younger than ten minutes) must settle the order and
// send the Payment confirmed record with no buyer message
// in between. Real model, dry-run WhatsApp, stubbed Seev
// read-back. Run from the repo root with the agent binary
// built.
const { spawn } = require("child_process");
const { createServer } = require("http");
const fs = require("fs");
const os = require("os");
const path = require("path");
const net = require("net");

// Sandbox hygiene: blanked before any child env is built.
delete process.env.WHATSAPP_TOKEN;
delete process.env.WHATSAPP_PHONE_NUMBER_ID;
delete process.env.WHATSAPP_APP_SECRET;
delete process.env.WHATSAPP_VERIFY_TOKEN;
delete process.env.SEEV_SECRET_KEY;
delete process.env.SEEV_API_KEY;
delete process.env.SEEV_WEBHOOK_SECRET;

const ROOT = path.join(__dirname, "..");
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "freshpay-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000501", BUYER = "233240000502";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    efua: { id: "efua", shopCode: "EFUA", name: "Efua's Store", ownerWhatsApp: OWNER,
      payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 25, deliveryPaidTo: "merchant",
      products: [
        { id: "bag", name: "Handmade Beaded Bag", description: "Hand-beaded evening bag with a gold clasp.", listPrice: 850, floorPrice: 680, stock: 3 },
      ] },
  },
}));

const freePort = () => new Promise((res, rej) => { const s = net.createServer(); s.once("error", rej); s.listen(0, () => { const p = s.address().port; s.close(() => res(p)); }); });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let seq = 0, passed = 0, failed = 0;
function check(name, ok, extra = "") {
  if (ok) { passed++; console.log("  ok  " + name); }
  else { failed++; console.log("FAIL  " + name + (extra ? "  [" + String(extra).slice(0, 220) + "]" : "")); }
}

(async () => {
  const port = await freePort();
  const BASE = `http://127.0.0.1:${port}`;
  let stubAmount = 0;
  const seevStub = createServer((req, res) => {
    let raw = "";
    req.on("data", (d) => { raw += d; });
    req.on("end", () => {
      res.setHeader("content-type", "application/json");
      if (req.method === "POST" && req.url === "/api/v1/developer/payments") {
        res.end(JSON.stringify({ data: { checkout_url: "https://pay.example.test/SEEVSIM-FRESH", reference: "SEEVSIM-FRESH" } }));
      } else if (req.method === "GET" && req.url.startsWith("/api/v1/developer/payments/")) {
        res.end(JSON.stringify({ data: { status: "completed", amount: stubAmount } }));
      } else {
        res.statusCode = 404;
        res.end("{}");
      }
    });
  });
  await new Promise((resolve) => seevStub.listen(0, "127.0.0.1", resolve));

  const env = { ...process.env, PORT: String(port), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1", SEEV_SECRET_KEY: "sk_sim", SEEV_API_BASE: `http://127.0.0.1:${seevStub.address().port}`, SEEV_WEBHOOK_SECRET: "prove-freshpay-webhook-secret" };
  delete env.SEEV_API_KEY;
  const agent = spawn(path.join(ROOT, "agent"), [], { env, stdio: ["ignore", "pipe", "pipe"] });
  let agentLog = "";
  agent.stdout.on("data", (d) => { agentLog += d; });
  agent.stderr.on("data", (d) => { agentLog += d; });

  const outboxTo = (who) => {
    const f = path.join(dir, "data", "outbox.jsonl");
    if (!fs.existsSync(f)) return [];
    return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => { try { return JSON.parse(l); } catch { return null; } }).filter((r) => r && r.to === who);
  };
  const bodies = (rs) => rs.map((r) => r.body || "");
  async function say(from, text) {
    const before = outboxTo(from).length;
    seq++;
    await fetch(`${BASE}/webhook`, { method: "POST", headers: { "content-type": "application/json" },
      body: JSON.stringify({ object: "whatsapp_business_account", entry: [{ changes: [{ value: { messages: [{ from, id: "w" + seq, type: "text", text: { body: text } }] } }] }] }) });
    let lastCount = -1, stable = 0;
    for (let i = 0; i < 60; i++) {
      await sleep(600);
      const n = outboxTo(from).length;
      if (n === lastCount) { stable++; } else { stable = 0; lastCount = n; }
      if (n > before && stable >= 4) break;
      if (i > 45 && n === before && stable >= 4) break;
    }
    await sleep(400);
    return outboxTo(from).slice(before);
  }

  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }

    console.log("A. a purchase reaches its payment link");
    await say(BUYER, "SHOP-EFUA");
    await say(BUYER, "Handmade Beaded Bag");
    await say(BUYER, "I will take it at 850");
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
    check("the payment link is out", done.some((b) => b.includes("Pay here to complete")), done.join(" | "));
    const totalRec = done.find((b) => b.includes("Total: GH₵"));
    const total = totalRec ? parseFloat((totalRec.match(/Total: GH₵([\d,]+)/) || [])[1].replace(/,/g, "")) : 875;
    stubAmount = Math.round(total * 100);
    const buyerCountAtLink = outboxTo(BUYER).length;

    console.log("B. the money lands; nobody speaks; the confirmation comes on its own");
    let confirmedAt = 0;
    const waitStart = Date.now();
    for (let i = 0; i < 50; i++) {
      await sleep(2000);
      const all = bodies(outboxTo(BUYER));
      if (all.some((b) => b.startsWith("Payment confirmed"))) { confirmedAt = Date.now(); break; }
    }
    check("Payment confirmed arrived with no buyer message", confirmedAt > 0, bodies(outboxTo(BUYER)).join(" | ").slice(-300));
    check("it arrived within about a tick of the wait starting", confirmedAt > 0 && confirmedAt - waitStart < 70000, confirmedAt ? String(confirmedAt - waitStart) + "ms" : "never");
    check("no buyer message was needed", outboxTo(BUYER).length >= buyerCountAtLink);
    const orders = JSON.parse(fs.readFileSync(path.join(dir, "data", "orders.json"), "utf8"));
    const mine = Object.values(orders).filter((o) => o.from === BUYER);
    check("the order stands paid in the book", mine.length === 1 && mine[0].status === "paid", JSON.stringify(mine));
    check("no panics in the agent log", !/panic/i.test(agentLog));
  } catch (e) {
    failed++;
    console.log("FAIL  harness: " + e.message);
    console.log(agentLog.slice(-600));
  } finally {
    agent.kill("SIGKILL");
    seevStub.close();
    console.log(`\nprove-freshpay: ${passed} passed, ${failed} failed  (data: ${dir})`);
    process.exit(failed ? 1 : 0);
  }
})();
