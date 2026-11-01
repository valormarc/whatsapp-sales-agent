#!/usr/bin/env node
// Live proof for the Seev webhook road, in Seev's REAL
// delivery shape. The agent's parser was written against
// a flat event body; Seev actually nests the payment in
// data.transaction and the order reference in
// transaction.metadata.developer.order_reference, so
// every real delivery arrived, verified, answered 200 —
// and settled nothing. This proof pays a sale to its
// link, then delivers a signed webhook in the exact real
// shape (captured from the provider's dashboard) and
// checks the order settles and the buyer is confirmed.
// Real model, dry-run WhatsApp, stubbed Seev read-back.
// Run from the repo root with the agent binary built.
const { spawn } = require("child_process");
const { createServer } = require("http");
const { createHmac: hmac } = require("crypto");
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
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "webhook-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000701", BUYER = "233240000702";
const WHSEC = "prove-webhook-secret";
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
        res.end(JSON.stringify({ data: { checkout_url: "https://pay.example.test/SEEVSIM-HOOK", reference: "SEEVSIM-HOOK" } }));
      } else if (req.method === "GET" && req.url.startsWith("/api/v1/developer/payments/")) {
        res.end(JSON.stringify({ data: { status: "completed", amount: stubAmount } }));
      } else {
        res.statusCode = 404;
        res.end("{}");
      }
    });
  });
  await new Promise((resolve) => seevStub.listen(0, "127.0.0.1", resolve));

  const env = { ...process.env, PORT: String(port), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1", SEEV_SECRET_KEY: "sk_sim", SEEV_API_BASE: `http://127.0.0.1:${seevStub.address().port}`, SEEV_WEBHOOK_SECRET: WHSEC };
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
    const refBody = bodies(outboxTo(BUYER)).find((b) => b.includes("Your order reference is")) || "";
    const ref = (refBody.match(/reference is (\S+)\./) || [])[1];
    check("the order reference is known", !!ref, refBody);
    const totalRec = done.find((b) => b.includes("Total: GH₵"));
    const total = totalRec ? parseFloat((totalRec.match(/Total: GH₵([\d,]+)/) || [])[1].replace(/,/g, "")) : 875;
    stubAmount = Math.round(total * 100);

    console.log("B. Seev delivers in its real shape; the order settles");
    const hookBody = JSON.stringify({
      event: "payment.succeeded", createdAt: new Date().toISOString(), env: "sandbox",
      data: { transaction: {
        reference: "SEEVSIM-HOOK", status: "completed", amount: total, currency: "GHS", type: "payment",
        metadata: { developer: { order_reference: ref }, origin: "developer_api", sandbox: true, sessionType: "checkout" },
      } },
    });
    const hookTs = `${Math.floor(Date.now() / 1000)}`;
    const hookSig = "v1=" + hmac("sha256", WHSEC).update(`${hookTs}.${hookBody}`).digest("hex");
    const hookRes = await fetch(`${BASE}/webhooks/seev`, { method: "POST", headers: { "content-type": "application/json", "x-seev-timestamp": hookTs, "x-seev-signature": hookSig }, body: hookBody });
    check("the delivery is acknowledged", hookRes.status === 200, String(hookRes.status));
    await sleep(1500);
    const buyerAll = bodies(outboxTo(BUYER));
    check("Payment confirmed reaches the buyer", buyerAll.some((b) => b.startsWith("Payment confirmed")), buyerAll.join(" | ").slice(-300));
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
    console.log(`\nprove-webhook: ${passed} passed, ${failed} failed  (data: ${dir})`);
    process.exit(failed ? 1 : 0);
  }
})();
