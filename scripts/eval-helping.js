#!/usr/bin/env node
// The helping test base. Scored against one sentence:
// a buyer should feel Stall is helping them get what they
// need, not merely showing products; a merchant should feel
// Stall is helping run the shop, not merely answering
// questions about it. Each leg observes behaviour and
// prints the decisive reply so the score can be checked
// against the words themselves. Gaps are reported as gaps,
// not failures: a leg marked GAP names a capability the
// build does not claim today. Real model, dry-run
// WhatsApp, stubbed Seev. Run from the repo root.
const { spawn } = require("child_process");
const { createServer } = require("http");
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
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "helping-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000801";
const B1 = "233240000811", B2 = "233240000812", B3 = "233240000813", B4 = "233240000814", MB = "233240000815", WB = "233240000816";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    efua: { id: "efua", shopCode: "EFUA", name: "Efua's Store", ownerWhatsApp: OWNER,
      payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 25, deliveryPaidTo: "merchant",
      products: [
        { id: "bag", name: "Handmade Beaded Bag", description: "Hand-beaded evening bag with a gold clasp.", listPrice: 850, floorPrice: 680, stock: 3 },
        { id: "dress", name: "Ankara Print Dress", description: "Bright ankara print dress, knee length, good for church and occasions.", listPrice: 220, floorPrice: 180, stock: 2 },
        { id: "sandals", name: "Leather Sandals", description: "Brown leather sandals, handmade.", listPrice: 150, floorPrice: 120, stock: 4 },
        { id: "scarf", name: "Silk Scarf", description: "Soft silk scarf in gold and green.", listPrice: 90, floorPrice: 70, stock: 5 },
      ] },
  },
}));

const freePort = () => new Promise((res, rej) => { const s = net.createServer(); s.once("error", rej); s.listen(0, () => { const p = s.address().port; s.close(() => res(p)); }); });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let seq = 0, passed = 0, failed = 0, gaps = 0;
function check(name, ok, extra = "") {
  if (ok) { passed++; console.log("  ok  " + name); }
  else { failed++; console.log("FAIL  " + name + (extra ? "  [" + String(extra).slice(0, 260) + "]" : "")); }
}
function gap(name, note) { gaps++; console.log(" GAP  " + name + "  [" + note + "]"); }

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
        res.end(JSON.stringify({ data: { checkout_url: "https://pay.example.test/SEEVSIM-HELP", reference: "SEEVSIM-HELP" } }));
      } else if (req.method === "GET" && req.url.startsWith("/api/v1/developer/payments/")) {
        res.end(JSON.stringify({ data: { status: "completed", amount: stubAmount } }));
      } else { res.statusCode = 404; res.end("{}"); }
    });
  });
  await new Promise((resolve) => seevStub.listen(0, "127.0.0.1", resolve));

  const env = { ...process.env, PORT: String(port), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1", SEEV_SECRET_KEY: "sk_sim", SEEV_API_BASE: `http://127.0.0.1:${seevStub.address().port}` };
  delete env.SEEV_API_KEY;
  delete env.SEEV_WEBHOOK_SECRET;
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
  const ordersBook = () => JSON.parse(fs.readFileSync(path.join(dir, "data", "orders.json"), "utf8"));

  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }

    console.log("BUYER: helping them get what they need, not merely showing products");
    console.log("B1. a need and a budget, no product named");
    await say(B1, "SHOP-EFUA");
    const b1 = bodies(await say(B1, "I need a gift for my mother. I have 200 cedis.")).join("\n");
    console.log("    reply: " + b1.replace(/\n/g, " | ").slice(0, 240));
    // The shelf block itself never counts as a recommendation:
    // strip fenced listings before judging what was put forward.
    const b1Prose = b1.replace(/```[\s\S]*?```/g, "");
    check("a specific in-budget item is put forward, not only the shelf", /Leather Sandals|Silk Scarf/.test(b1Prose) && /150|90/.test(b1Prose), b1);

    console.log("B2. a price balk turns into a cheaper way to get it");
    await say(B2, "SHOP-EFUA");
    await say(B2, "Handmade Beaded Bag");
    const b2 = bodies(await say(B2, "That is too expensive for me.")).join("\n");
    console.log("    reply: " + b2.replace(/\n/g, " | ").slice(0, 240));
    check("a cheaper alternative is named", /Ankara Print Dress|Leather Sandals|Silk Scarf/.test(b2), b2);

    console.log("B3. a use case, in the buyer's own words");
    await say(B3, "SHOP-EFUA");
    const b3 = bodies(await say(B3, "I need something to wear to church on Sunday.")).join("\n");
    console.log("    reply: " + b3.replace(/\n/g, " | ").slice(0, 240));
    check("the dress is put forward for the occasion", /Ankara Print Dress/.test(b3), b3);

    console.log("B4. the need is carried through to the payment link");
    await say(B4, "SHOP-EFUA");
    await say(B4, "Silk Scarf");
    await say(B4, "I will take it at 90");
    let done = [];
    for (let i = 0; i < 6; i++) {
      const lastQ = (bodies(outboxTo(B4)).pop() || "").toLowerCase();
      let answer = "yes";
      if (lastQ.includes("where should it go") || lastQ.includes("where are you")) answer = "Osu";
      else if (lastQ.includes("what name should go")) answer = "Efua Mensima";
      else if (lastQ.includes("any message for the seller")) answer = "no";
      done = bodies(await say(B4, answer));
      if (done.some((b) => b.includes("Pay here to complete"))) break;
    }
    check("the payment link is reached, asking only what is missing", done.some((b) => b.includes("Pay here to complete")), done.join(" | "));

    console.log("MERCHANT: helping run the shop, not merely answering questions");
    console.log("M2. money lands; nobody asks; both sides are told");
    await say(MB, "SHOP-EFUA");
    await say(MB, "Leather Sandals");
    await say(MB, "I will take it at 150");
    const ownerBefore = outboxTo(OWNER).length;
    let mDone = [];
    for (let i = 0; i < 6; i++) {
      const lastQ = (bodies(outboxTo(MB)).pop() || "").toLowerCase();
      let answer = "yes";
      if (lastQ.includes("where should it go") || lastQ.includes("where are you")) answer = "Osu";
      else if (lastQ.includes("what name should go")) answer = "Efua Mensima";
      else if (lastQ.includes("any message for the seller")) answer = "no";
      mDone = bodies(await say(MB, answer));
      if (mDone.some((b) => b.includes("Pay here to complete"))) break;
    }
    const totalRec = mDone.find((b) => b.includes("Total: GH₵"));
    const total = totalRec ? parseFloat((totalRec.match(/Total: GH₵([\d,]+)/) || [])[1].replace(/,/g, "")) : 175;
    stubAmount = Math.round(total * 100);
    const buyerBefore = outboxTo(MB).length;
    let settled = false;
    for (let i = 0; i < 40; i++) {
      await sleep(2500);
      const mine = Object.values(ordersBook()).filter((o) => o.from === MB);
      if (mine.length && mine[0].status === "paid") { settled = true; break; }
    }
    check("the order settles with no one speaking", settled);
    check("the buyer is confirmed unaided", bodies(outboxTo(MB)).slice(buyerBefore).some((b) => b.startsWith("Payment confirmed")));
    check("the merchant is told unaided", bodies(outboxTo(OWNER)).slice(ownerBefore).some((b) => /Total paid|Payment: confirmed/.test(b)));
    const myOrder = Object.values(ordersBook()).find((o) => o.from === MB);
    const myRef = myOrder && myOrder.reference;

    console.log("M1. attention is a work list, not a figure dump");
    const m1 = bodies(await say(OWNER, "What needs my attention today?")).join("\n");
    console.log("    reply: " + m1.replace(/\n/g, " | ").slice(0, 260));
    check("the paid order waiting to go out is named as work", /Leather Sandals/.test(m1) && /sent|dispatch|deliver|waiting/i.test(m1), m1);

    console.log("M5. the merchant moves the order on in her own words");
    const buyerBeforeM5 = outboxTo(MB).length;
    await say(OWNER, `Order ${myRef} has been delivered`);
    const after = ordersBook()[myRef] || Object.values(ordersBook()).find((o) => o.from === MB) || {};
    console.log("    order state: " + JSON.stringify({ status: after.status, deliveryState: after.deliveryState }));
    check("the book records the order as sent or delivered", /sent|delivered/i.test(String(after.deliveryState || "")), JSON.stringify(after));
    const buyerHeard = bodies(outboxTo(MB)).slice(buyerBeforeM5);
    console.log("    buyer heard: " + (buyerHeard.join(" | ").slice(0, 200) || "(nothing)"));

    console.log("M3. a want is carried to the merchant at the moment of asking");
    const ownerBeforeM3 = outboxTo(OWNER).length;
    await say(WB, "SHOP-EFUA");
    await say(WB, "Do you have purple kente cloth?");
    await sleep(800);
    check("the merchant hears about the want unaided", bodies(outboxTo(OWNER)).slice(ownerBeforeM3).some((b) => /kente/i.test(b)), bodies(outboxTo(OWNER)).slice(ownerBeforeM3).join(" | "));

    // M4, the push leg, lives in prove-dispatch.js: the
    // wait it proves is three hours, longer than this base
    // runs, so it seeds a stale book and watches the agent
    // wake instead. It passes there.
    console.log("M4. a paid order waiting too long is pushed to the merchant on Stall's own initiative");
    console.log("    covered by prove-dispatch.js (the three hour wait outlasts this base)");

    check("no panics in the agent log", !/panic/i.test(agentLog));
  } catch (e) {
    failed++;
    console.log("FAIL  harness: " + e.message);
    console.log(agentLog.slice(-600));
  } finally {
    agent.kill("SIGKILL");
    seevStub.close();
    console.log(`\neval-helping: ${passed} passed, ${failed} failed, ${gaps} gaps named  (data: ${dir})`);
    process.exit(failed ? 1 : 0);
  }
})();
