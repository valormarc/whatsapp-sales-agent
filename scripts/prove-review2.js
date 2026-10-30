#!/usr/bin/env node
// Live proof for the review fixes that face the model: the
// widened walk price shapes (GHS before the figure, the
// market's paa, the k thousand), the cheapest ordering
// enforced in code on the discovery road, and a picture ask
// with nothing in view asking "which one?" instead of
// dumping the catalogue. Real model, dry-run WhatsApp,
// placeholder payment rail. Run from the repo root with
// the agent binary built.
const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");
const net = require("net");

const ROOT = path.join(__dirname, "..");
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "review2-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const WALKOWNER = "233240000501", BUYER = "233240000502", BUYER2 = "233240000503";
const catalogue = { merchants: {
  walk: { id: "walk", name: "Walk-In Goods", shopCode: "WALK", ownerWhatsApp: WALKOWNER, payoutPhone: WALKOWNER, dailyGiveawayAllowance: 300,
    products: [] },
  ashop: { id: "ashop", name: "Ama's Heels", shopCode: "AMA", ownerWhatsApp: "233240000509", payoutPhone: "233240000509", dailyGiveawayAllowance: 300,
    products: [{ id: "stiletto", name: "Stiletto Heel", description: "Tall stiletto heel.", listPrice: 300, floorPrice: 250, stock: 2 }] },
  zshop: { id: "zshop", name: "Zed's Heels", shopCode: "ZED", ownerWhatsApp: "233240000510", payoutPhone: "233240000510", dailyGiveawayAllowance: 300, deliveryFee: 20, deliveryPaidTo: "merchant",
    products: [{ id: "blockheel", name: "Block Heel", description: "Low block heel.", listPrice: 120, floorPrice: 100, stock: 2 }] },
} };
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify(catalogue));

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
async function main() {
  const port = await freePort();
  const base = "http://127.0.0.1:" + port;
  const env = { ...process.env, WHATSAPP_DRY_RUN: "1", WHATSAPP_APP_SECRET: "", HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), PORT: String(port) };
  delete env.SEEV_API_KEY; delete env.SEEV_SECRET_KEY; delete env.SEEV_WEBHOOK_SECRET; delete env.WHATSAPP_TOKEN;
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
  const shelf = () => JSON.parse(fs.readFileSync(path.join(dir, "data", "catalogue.json"), "utf8")).merchants.walk.products;
  async function walkProduct(name, desc, listAnswer, lastAnswer) {
    await post(WALKOWNER, "add a product");
    await waitFor(WALKOWNER, (t) => /called|name/i.test(t), "the name ask");
    await post(WALKOWNER, name);
    await waitFor(WALKOWNER, () => true, "the description ask", 20000).catch(() => {});
    await post(WALKOWNER, desc);
    await waitFor(WALKOWNER, (t) => t.includes("list price"), "the list price ask");
    await post(WALKOWNER, listAnswer);
    const rs = await waitFor(WALKOWNER, (t) => t.includes("last price") || t.includes("did not take"), "the last price ask");
    if (!body(rs).includes("last price")) throw new Error("the list answer was refused: " + body(rs));
    await post(WALKOWNER, lastAnswer);
    // Whatever the walk asks next (measure, then the summary), answer through to the save.
    for (let i = 0; i < 6; i++) {
      const got = body(fresh(WALKOWNER));
      if (/by measure/i.test(got)) { await post(WALKOWNER, "only as a whole"); continue; }
      if (/How much of it/i.test(got)) { await post(WALKOWNER, "1"); continue; }
      if (/Shall I add it/i.test(got)) { await post(WALKOWNER, "yes"); continue; }
      const more = await waitFor(WALKOWNER, () => true, "the next walk step", 30000).catch(() => []);
      if (!more.length) break;
    }
    await new Promise((r) => setTimeout(r, 1200));
  }
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(base + "/healthz"); if (r.ok) break; } catch {} await new Promise((r) => setTimeout(r, 500)); }

    console.log("A. the widened walk shapes settle their steps");
    await walkProduct("Canvas Tote", "A sturdy canvas tote.", "GHS 500", "450 paa");
    let p = shelf().find((x) => x.name === "Canvas Tote");
    check("GHS 500 lists at 500 and 450 paa lasts at 450", !!p && p.listPrice === 500 && p.floorPrice === 450, JSON.stringify(p));
    await walkProduct("Leather Belt", "A plain leather belt.", "2k", "1500");
    p = shelf().find((x) => x.name === "Leather Belt");
    check("2k lists at 2000", !!p && p.listPrice === 2000 && p.floorPrice === 1500, JSON.stringify(p));

    console.log("B. the cheapest heel leads on the discovery road");
    await post(BUYER, "What is the cheapest heel?");
    let rs = await waitFor(BUYER, (t) => t.includes("Heel"), "the heel answers");
    const text = body(rs);
    const cheapAt = text.indexOf("Block Heel"), dearAt = text.indexOf("Stiletto Heel");
    check("the GH₵120 heel is the one presented first", cheapAt >= 0 && (dearAt < 0 || cheapAt < dearAt), text);

    console.log("C. a picture ask with nothing in view asks which one");
    await post(BUYER2, "SHOP-ZED");
    await waitFor(BUYER2, (t) => t.includes("Zed"), "the shop welcome");
    fresh(BUYER2);
    await post(BUYER2, "send pic");
    rs = await waitFor(BUYER2, (t) => t.includes("Which one"), "the which-one ask");
    check("she is asked which one, and the shelf is shown", body(rs).includes("Which one would you like to see?"), body(rs));

    check("no panics in the agent log", !/panic/i.test(agentLog));
  } catch (e) {
    failed++;
    console.log("FAIL  harness: " + e.message);
    console.log(agentLog.slice(-800));
  } finally {
    agent.kill("SIGTERM");
    console.log(`\nprove-review2: ${passed} passed, ${failed} failed  (data: ${dir})`);
    process.exit(failed ? 1 : 0);
  }
}
main();
