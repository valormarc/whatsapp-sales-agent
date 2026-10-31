#!/usr/bin/env node
// Live proof for the kept-photo cap: the same dress photo
// sent seven times under seven media ids (an album that
// outruns the cap). Six photos are kept; the seventh earns
// an honest "I could not add that one" instead of a false
// "Photo added". Real model, real WhatsApp media, dry-run
// sends. Needs CAP_MEDIA_1..7 and WHATSAPP_TOKEN; without
// them it is skipped, not faked.
const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");
const net = require("net");

// Sandbox hygiene: a dry-run child must never inherit live
// send or settle credentials from the operator's shell. This
// proof keeps the WhatsApp token — the sandbox needs it to
// download the real media it is fed — but the phone number
// id (the sending identity) and the Seev credentials are
// blanked, so the child cannot send or settle anything.
delete process.env.WHATSAPP_PHONE_NUMBER_ID;
delete process.env.WHATSAPP_APP_SECRET;
delete process.env.WHATSAPP_VERIFY_TOKEN;
delete process.env.SEEV_SECRET_KEY;
delete process.env.SEEV_API_KEY;
delete process.env.SEEV_WEBHOOK_SECRET;

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
  const media = [1,2,3,4,5,6,7].map((i) => process.env["CAP_MEDIA_" + i]);
  if (media.some((m) => !m) || !process.env.WHATSAPP_TOKEN) {
    console.log("  --  skipped: CAP_MEDIA_1..7 / WHATSAPP_TOKEN not in env");
    return;
  }
  const port = await freePort();
  const base = "http://127.0.0.1:" + port;
  const env = { ...process.env, WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1", HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), PORT: String(port) };
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

    console.log("1. seven sends of one dress photo");
    await photo(OWNER, media[0], "Available. @130ghc");
    let rs = await waitFor(OWNER, (t) => t.includes("last price"), "the compact read-back");
    check("the first photo opens the listing", body(rs).includes("130"), body(rs));
    for (let i = 1; i < 6; i++) {
      await photo(OWNER, media[i], "Available. @130ghc");
      rs = await waitFor(OWNER, (t) => t.includes("Photo added"), "photo " + (i + 1) + " joining");
    }
    check("photos two to six are added", true);
    await photo(OWNER, media[6], "Available. @130ghc");
    rs = await waitFor(OWNER, (t) => t.includes("could not add"), "the honest cap line");
    check("the seventh photo is honestly refused, not claimed as added", body(rs).includes("already holds 6 photos"), body(rs));

    console.log("2. the save keeps exactly six");
    await say(OWNER, "120");
    rs = await waitFor(OWNER, (t) => t.includes("Saved"), "the save");
    const saved = JSON.parse(fs.readFileSync(path.join(dir, "data", "catalogue.json"), "utf8"));
    const products = saved.merchants.luxe.products || [];
    check("exactly one product was saved", products.length === 1, JSON.stringify(products.map((p) => p.name)));
    if (products.length === 1) {
      check("six photos are kept on the listing", (products[0].photos || []).length === 6, String((products[0].photos || []).length));
    }
  } catch (e) {
    failed++;
    console.log("FAIL  " + e.message);
    console.log(agentLog.slice(-600));
  } finally {
    agent.kill();
  }
  console.log("\nphoto cap proof: " + passed + " passed, " + failed + " failed");
  process.exit(failed ? 1 : 0);
}
main();
