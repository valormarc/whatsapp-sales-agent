#!/usr/bin/env node
// Live proof for the colour rule, run on the exact shape
// that produced it: a seller forwards three photos of one
// dress in three colours, each captioned with the same
// price. The listing must save as ONE product whose name
// carries no single colour and whose description lists the
// colours it comes in. Real model, real WhatsApp media,
// dry-run sends, placeholder payment rail. Needs
// COLOUR_MEDIA_1..3 (WhatsApp media ids for the three
// photos) and a WHATSAPP_TOKEN the agent can download them
// with; without them it is skipped, not faked.
const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");
const net = require("net");

const ROOT = path.join(__dirname, "..");
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "colours-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000601";
const catalogue = { merchants: {
  luxe: { id: "luxe", name: "Luxe", shopCode: "LUXE", ownerWhatsApp: OWNER, payoutPhone: OWNER, dailyGiveawayAllowance: 300, products: [] },
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
  const media = [process.env.COLOUR_MEDIA_1, process.env.COLOUR_MEDIA_2, process.env.COLOUR_MEDIA_3];
  if (media.some((m) => !m) || !process.env.WHATSAPP_TOKEN) {
    console.log("  --  skipped: COLOUR_MEDIA_1..3 / WHATSAPP_TOKEN not in env");
    return;
  }
  const port = await freePort();
  const base = "http://127.0.0.1:" + port;
  const env = { ...process.env, WHATSAPP_DRY_RUN: "1", HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), PORT: String(port) };
  delete env.SEEV_API_KEY; delete env.SEEV_SECRET_KEY; delete env.SEEV_WEBHOOK_SECRET;
  const agent = spawn(path.join(ROOT, "agent"), [], { cwd: dir, env, stdio: ["ignore", "pipe", "pipe"] });
  let agentLog = "";
  agent.stdout.on("data", (d) => { agentLog += d; });
  agent.stderr.on("data", (d) => { agentLog += d; });
  const post = async (from, msg) => {
    const res = await fetch(base + "/webhook", { method: "POST", headers: { "content-type": "application/json" },
      body: JSON.stringify({ entry: [{ changes: [{ value: { messages: [msg] } }] }] }) });
    await res.text();
  };
  const say = (from, text) => post(from, { from, id: "w" + ++seq, timestamp: String(Math.floor(Date.now() / 1000)), type: "text", text: { body: text } });
  const photo = (from, id, caption) => post(from, { from, id: "w" + ++seq, timestamp: String(Math.floor(Date.now() / 1000)), type: "image", image: { id, caption } });
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(base + "/healthz"); if (r.ok) break; } catch {} await new Promise((r) => setTimeout(r, 500)); }

    console.log("1. three colours of one dress, one price");
    await photo(OWNER, media[0], "Available. @130ghc");
    let rs = await waitFor(OWNER, (t) => t.includes("last price"), "the compact read-back");
    check("the first photo opens one listing at GH₵130", body(rs).includes("130"), body(rs));
    await photo(OWNER, media[1], "Available. @130ghc");
    rs = await waitFor(OWNER, (t) => t.includes("Photo added"), "the second colour joining");
    check("the second colour joins the same listing", true);
    await photo(OWNER, media[2], "Available. @130ghc");
    rs = await waitFor(OWNER, (t) => t.includes("Photo added"), "the third colour joining");
    check("the third colour joins the same listing", true);

    console.log("2. the save, under the colour rule");
    await say(OWNER, "120");
    rs = await waitFor(OWNER, (t) => t.includes("Saved"), "the save");
    const saved = JSON.parse(fs.readFileSync(path.join(dir, "data", "catalogue.json"), "utf8"));
    const products = saved.merchants.luxe.products || [];
    check("exactly one product was saved", products.length === 1, JSON.stringify(products.map((p) => p.name)));
    if (products.length === 1) {
      const p = products[0];
      console.log("  saved: " + JSON.stringify({ name: p.name, description: p.description, photos: (p.photos || []).length }));
      const m = /comes in ([^.]+)\./i.exec(p.description || "");
      check("the description lists the colours it comes in", !!m, p.description);
      const colours = m ? m[1].toLowerCase().split(/,| and /).map((s) => s.trim()).filter(Boolean) : [];
      check("at least two colours were read across the photos", colours.length >= 2, colours.join(","));
      const nameWords = (p.name || "").toLowerCase().split(/[^a-z]+/);
      check("the name carries none of those colours", colours.every((c) => !nameWords.includes(c)), p.name);
      check("all three photos are kept on the listing", (p.photos || []).length === 3, String((p.photos || []).length));
      check("the prices are the seller's own", p.listPrice === 130 && p.floorPrice === 120, p.listPrice + "/" + p.floorPrice);
    }
  } catch (e) {
    failed++;
    console.log("FAIL  " + e.message);
    console.log(agentLog.slice(-600));
  } finally {
    agent.kill();
  }
  console.log("\ncolours proof: " + passed + " passed, " + failed + " failed");
  process.exit(failed ? 1 : 0);
}
main();
