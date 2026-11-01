#!/usr/bin/env node
// Live proof for the walk price gate: a listing walk
// waiting on a price must not eat the merchant's other
// business. His live loop: mid-listing for a Lace
// Nightdress, every errand — a delivery report, an
// attention question, a stock question — came back as
// "What is the list price for Lace Nightdress?". At a
// price step, a message not shaped as the price is read
// by her ordinary readers first (commands, day questions,
// the mind's acts) and the walk stays open behind them
// for the figure that resumes it. Real model, dry-run
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
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "walkgate-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000601";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    efua: { id: "efua", name: "Efua's Store", shopCode: "EFUA", ownerWhatsApp: OWNER, payoutPhone: OWNER,
      dailyGiveawayAllowance: 300, deliveryFee: 20, deliveryPaidTo: "merchant",
      products: [{ id: "bag", name: "Handmade Beaded Bag", description: "Hand-beaded evening bag with a gold clasp.", listPrice: 850, floorPrice: 680, stock: 3 }] },
  },
}));

const freePort = () => new Promise((res, rej) => { const s = net.createServer(); s.once("error", rej); s.listen(0, () => { const p = s.address().port; s.close(() => res(p)); }); });
let seq = 0, passed = 0, failed = 0;
const seen = new Map();
function outbox(to) {
  const f = path.join(dir, "data", "outbox.jsonl");
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

(async () => {
  const port = await freePort();
  const base = "http://127.0.0.1:" + port;
  const env = { ...process.env, WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1", HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), PORT: String(port) };
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

    console.log("A. the walk reaches its price step");
    await post(OWNER, "add a product");
    await waitFor(OWNER, (t) => /called|name/i.test(t), "the name question");
    await post(OWNER, "Lace Nightdress");
    await waitFor(OWNER, (t) => t.includes("Tell me about"), "the description question");
    await post(OWNER, "A lace nightdress, soft and light.");
    let rs = await waitFor(OWNER, (t) => t.includes("list price for Lace Nightdress"), "the list price question");
    check("the walk stands at the list price step", true);

    console.log("B. an errand at the price step is served, not eaten");
    await post(OWNER, "What needs my attention today?");
    rs = await waitFor(OWNER, (t) => t.includes("in progress") || /attention/i.test(t), "the attention answer");
    check("the attention question gets its own answer", !body(rs).includes("could not read that as a price"), body(rs));

    console.log("C. the figure resumes the walk where it left off");
    await post(OWNER, "120");
    rs = await waitFor(OWNER, (t) => /last price/i.test(t), "the last price question");
    check("the walk asks its next question", body(rs).toLowerCase().includes("last price for lace nightdress"), body(rs));
    await post(OWNER, "100");
    rs = await waitFor(OWNER, (t) => /measure|whole|Shall I add/i.test(t), "the walk moving on");
    check("the walk moves on past the last price", true);
    check("no panics in the agent log", !/panic/i.test(agentLog));
  } catch (e) {
    failed++;
    console.log("FAIL  harness: " + e.message);
    console.log(agentLog.slice(-800));
  } finally {
    agent.kill("SIGTERM");
    console.log(`\nprove-walkgate: ${passed} passed, ${failed} failed  (data: ${dir})`);
    process.exit(failed ? 1 : 0);
  }
})();
