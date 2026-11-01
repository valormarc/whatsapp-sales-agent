#!/usr/bin/env node
// Live proof for the dispatch watch: a paid order that has
// waited hours to go out is pushed to the merchant on
// Stall's own initiative, once, with nobody speaking. The
// book is seeded with two paid orders before the agent
// starts — one five hours old and unsent, one half an
// hour old — and only the stale one may be pushed. Real
// binary, real books, dry-run WhatsApp; no model turn is
// involved, because the watch speaks from the record.
// Run from the repo root with the agent binary built.
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
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "dispatch-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000901";
const ago = (h) => new Date(Date.now() - h * 3600 * 1000).toISOString().replace(/\.\d+Z$/, "Z");
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    efua: { id: "efua", shopCode: "EFUA", name: "Efua's Store", ownerWhatsApp: OWNER,
      payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 25, deliveryPaidTo: "merchant",
      products: [
        { id: "sandals", name: "Leather Sandals", description: "Brown leather sandals, handmade.", listPrice: 150, floorPrice: 120, stock: 4 },
      ] },
  },
}));
const order = (ref, from, createdAt) => ({
  reference: ref, status: "paid", from, productName: "Leather Sandals", price: 150,
  deliveryFee: 25, total: 175, providerRef: "SEEVSIM-DISP", merchantId: "efua",
  payoutPhone: OWNER, fulfilment: "delivery", location: "Osu", packageName: "Efua Mensima",
  deliveryPaidTo: "merchant", bookedList: 150, createdAt, linkAttempts: 1,
});
fs.writeFileSync(path.join(dir, "data", "orders.json"), JSON.stringify({
  "EFUA-DISP-STALE": order("EFUA-DISP-STALE", "233240000902", ago(5)),
  "EFUA-DISP-FRESH": order("EFUA-DISP-FRESH", "233240000903", ago(0.5)),
}, null, 1));

const freePort = () => new Promise((res, rej) => { const s = net.createServer(); s.once("error", rej); s.listen(0, () => { const p = s.address().port; s.close(() => res(p)); }); });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let passed = 0, failed = 0;
function check(name, ok, extra = "") {
  if (ok) { passed++; console.log("  ok  " + name); }
  else { failed++; console.log("FAIL  " + name + (extra ? "  [" + String(extra).slice(0, 240) + "]" : "")); }
}

(async () => {
  const port = await freePort();
  const BASE = `http://127.0.0.1:${port}`;
  const env = { ...process.env, PORT: String(port), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1" };
  const agent = spawn(path.join(ROOT, "agent"), [], { env, stdio: ["ignore", "pipe", "pipe"] });
  let agentLog = "";
  agent.stdout.on("data", (d) => { agentLog += d; });
  agent.stderr.on("data", (d) => { agentLog += d; });
  const outboxTo = (who) => {
    const f = path.join(dir, "data", "outbox.jsonl");
    if (!fs.existsSync(f)) return [];
    return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => { try { return JSON.parse(l); } catch { return null; } }).filter((r) => r && r.to === who);
  };

  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }
    console.log("A. the watch wakes with the agent and pushes the stale order");
    let nudges = [];
    for (let i = 0; i < 30; i++) {
      await sleep(2000);
      nudges = outboxTo(OWNER).map((r) => r.body || "");
      if (nudges.some((b) => b.includes("EFUA-DISP-STALE"))) break;
    }
    check("the stale paid order is pushed to the merchant unaided", nudges.some((b) => b.includes("EFUA-DISP-STALE") && b.includes("waiting to go out")), nudges.join(" | "));
    check("the fresh paid order is left alone", !nudges.some((b) => b.includes("EFUA-DISP-FRESH")), nudges.join(" | "));
    const book = JSON.parse(fs.readFileSync(path.join(dir, "data", "orders.json"), "utf8"));
    check("the pushed order is marked so it is never pushed twice", book["EFUA-DISP-STALE"].dispatchNudged === true, JSON.stringify(book["EFUA-DISP-STALE"]));
    check("no panics in the agent log", !/panic/i.test(agentLog));
  } catch (e) {
    failed++;
    console.log("FAIL  harness: " + e.message);
    console.log(agentLog.slice(-600));
  } finally {
    agent.kill("SIGKILL");
    console.log(`\nprove-dispatch: ${passed} passed, ${failed} failed  (data: ${dir})`);
    process.exit(failed ? 1 : 0);
  }
})();
