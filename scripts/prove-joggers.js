#!/usr/bin/env node
// Live proof for the joggers fixes, from his own live
// retest on the real number (2026-11-01): a photo caption
// "Please joggers are available 120gh / Only few in stock
// please / Very heavy and thick" put a product on the
// shelf NAMED "Please joggers are available"; the sold
// out answer to the buyer dumped the whole shelf as text
// under a doubled heading instead of presenting the shelf
// sheet; and when the merchant said "I have 3 joggers in
// stock" she was told stock cannot be tracked, which is
// false — the shelf runs on counts the agent itself
// keeps. This proof walks the same roads with the real
// model: the announcement is stripped to the goods' name,
// a stated count is applied to the shelf product, the
// sold out answer ends in the sheet, and a direct question
// about stock tracking is answered truthfully. Dry-run
// WhatsApp, stubbed nothing else. Run from the repo root
// with the agent binary built.
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
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "joggers-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000701";
const BUYER = "233240000702";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    efua: { id: "efua", name: "Efua's Store", shopCode: "EFUA", ownerWhatsApp: OWNER, payoutPhone: OWNER,
      dailyGiveawayAllowance: 300, deliveryFee: 20, deliveryPaidTo: "merchant",
      products: [
        { id: "sandals", name: "Leather Sandals", description: "Brown handmade leather sandals.", listPrice: 150, floorPrice: 120, stock: 2 },
        { id: "scarf", name: "Silk Scarf", description: "A soft silk scarf.", listPrice: 90, floorPrice: 70, stock: 2 },
      ] },
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
function catalogue() { return JSON.parse(fs.readFileSync(path.join(dir, "data", "catalogue.json"), "utf8")); }
function joggers() { return (catalogue().merchants.efua.products || []).find((p) => /jogger/i.test(p.name)); }
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
  else { failed++; console.log("FAIL  " + name + (extra ? "  [" + String(extra).slice(0, 240) + "]" : "")); }
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

    console.log("A. the announcement is stripped to the goods' name");
    await post(OWNER, "add a product");
    await waitFor(OWNER, (t) => /called/i.test(t), "the name question");
    await post(OWNER, "Please joggers are available 120gh\nOnly few in stock please\nVery heavy and thick");
    let rs = await waitFor(OWNER, (t) => /last price/i.test(t), "the last price question");
    check("the walk asks the last price for joggers", /last price for joggers/i.test(body(rs)), body(rs));
    check("no question carries the announcement as a name", !/for Please joggers are available/i.test(body(rs)), body(rs));

    console.log("B. the listing completes under the clean name");
    await post(OWNER, "110");
    rs = await waitFor(OWNER, (t) => /measure|whole|Shall I add/i.test(t), "the walk moving on");
    if (/measure|whole/i.test(body(rs)) && !/Shall I add/i.test(body(rs))) {
      await post(OWNER, "only as a whole");
      rs = await waitFor(OWNER, (t) => /Shall I add/i.test(t), "the confirm question");
    }
    await post(OWNER, "yes");
    await waitFor(OWNER, (t) => /Saved|is live/i.test(t), "the save line");
    const jp = joggers();
    check("the saved product is named joggers", !!jp && jp.name === "joggers", jp && jp.name);
    check("its prices are hers (120 over 110)", !!jp && jp.listPrice === 120 && jp.floorPrice === 110, jp && (jp.listPrice + "/" + jp.floorPrice));
    check("no count was stated, so the save assumes one", !!jp && jp.stock === 1, jp && jp.stock);

    console.log("C. her stated count is applied, not lectured");
    await post(OWNER, "I have 3 joggers in stock");
    rs = await waitFor(OWNER, (t) => t.includes("in stock"), "the stock confirmation");
    check("the confirmation names the new count", body(rs).includes("now has 3 in stock"), body(rs));
    check("the shelf record carries 3", joggers() && joggers().stock === 3, joggers() && joggers().stock);

    console.log("D. the sold out answer ends in the shelf sheet");
    await post(OWNER, "set stock of joggers to 0");
    await waitFor(OWNER, (t) => /none in stock/i.test(t), "the zero confirmation");
    await post(BUYER, "SHOP-EFUA");
    await waitFor(BUYER, (t) => t.includes("Efua"), "the entry");
    fresh(BUYER);
    await post(BUYER, "I need joggers my budget is 100");
    rs = await waitFor(BUYER, (t) => /sold out/i.test(t), "the sold out answer");
    check("the buyer is told the truth", /sold out just now/i.test(body(rs)), body(rs));
    const sheets = rs.filter((r) => r.kind === "list");
    check("the shelf arrives as a sheet, not a text dump", sheets.length >= 1 && !body(rs).includes("```"), body(rs).slice(0, 200));
    const rows = sheets.flatMap((r) => (r.sections || []).flatMap((s) => (s.rows || []).map((x) => x.title)));
    check("the sheet carries the stocked goods", rows.some((x) => /Leather Sandals/i.test(x)), rows.join(", "));

    console.log("E. asked plainly, stock tracking is answered truthfully");
    await post(OWNER, "can you track my stock?");
    rs = await waitFor(OWNER, (t) => t.trim().length > 10, "the stock answer");
    check("no claim that stock cannot be tracked", !/cannot track|can't track|can not track/i.test(body(rs)), body(rs));
    console.log("     her answer: " + body(rs).replace(/\n/g, " / ").slice(0, 220));
    check("no panics in the agent log", !/panic/i.test(agentLog));
  } catch (e) {
    failed++;
    console.log("FAIL  harness: " + e.message);
    console.log(agentLog.slice(-800));
  } finally {
    agent.kill("SIGTERM");
    console.log(`\nprove-joggers: ${passed} passed, ${failed} failed  (data: ${dir})`);
    process.exit(failed ? 1 : 0);
  }
})();
