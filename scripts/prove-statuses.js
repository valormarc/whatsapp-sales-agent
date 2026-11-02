#!/usr/bin/env node
// Live-shaped proof for the status capture: Meta's
// reports about the messages this number sends
// (delivered, read, failed) arrive on the same webhook
// as inbound messages, and until the payload declared
// the statuses field every one was dropped unseen. This
// proof starts the agent in its sandbox, posts an
// inbound message and then status webhooks in Meta's
// documented shape — a delivered, a read, the same
// delivered again (Meta retries and batches), and a
// failure carrying its error — and checks the status
// book and journal on disk: the rollup advanced, the
// duplicate changed nothing, the failure kept its
// error, and the inbound turn set the heard-from clock.
// Genuine status events only ever come from real
// WhatsApp traffic; this proves the shape end to end
// without a single real send. Run from the repo root
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
delete process.env.META_MODEL_API_KEY;

const ROOT = path.join(__dirname, "..");
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "statuses-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const BUYER = "233240000961";

const freePort = () => new Promise((res, rej) => { const s = net.createServer(); s.once("error", rej); s.listen(0, () => { const p = s.address().port; s.close(() => res(p)); }); });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let passed = 0, failed = 0;
function check(name, ok, extra = "") {
  if (ok) { passed++; console.log("  ok  " + name); }
  else { failed++; console.log("FAIL  " + name + (extra ? "  [" + String(extra).slice(0, 220) + "]" : "")); }
}

async function post(base, payload) {
  const r = await fetch(base + "/webhook", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(payload) });
  if (r.status !== 200) throw new Error("webhook answered " + r.status);
  await r.text();
}

const wrap = (value) => ({ object: "whatsapp_business_account", entry: [{ id: "100000000000001", changes: [{ field: "messages", value: { messaging_product: "whatsapp", metadata: { display_phone_number: "233200000000", phone_number_id: "100000000000002" }, ...value } }] }] });

(async () => {
  const port = await freePort();
  const BASE = `http://127.0.0.1:${port}`;
  const env = { ...process.env, PORT: String(port), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1" };
  const agent = spawn(path.join(ROOT, "agent"), [], { env, stdio: ["ignore", "pipe", "pipe"] });
  let agentLog = "";
  agent.stdout.on("data", (d) => { agentLog += d; });
  agent.stderr.on("data", (d) => { agentLog += d; });
  try {
    for (let i = 0; i < 50; i++) {
      try { const h = await fetch(BASE + "/healthz"); if (h.ok) break; } catch (e) { /* not up yet */ }
      await sleep(200);
    }

    // One inbound message: her turn must set the
    // heard-from clock in the status book.
    await post(BASE, wrap({ messages: [{ from: BUYER, id: "wamid.prove-inbound", type: "text", timestamp: "1700002000", text: { body: "Hello" } }] }));

    // The status reports, in Meta's documented shape.
    const T1 = 1700002100, T2 = 1700002160;
    await post(BASE, wrap({ statuses: [
      { id: "wamid.prove-1", status: "delivered", timestamp: String(T1), recipient_id: BUYER },
      { id: "wamid.prove-1", status: "read", timestamp: String(T2), recipient_id: BUYER },
      { id: "wamid.prove-1", status: "delivered", timestamp: String(T1), recipient_id: BUYER },
      { id: "wamid.prove-2", status: "failed", timestamp: String(T1 + 30), recipient_id: BUYER, errors: [{ code: 131047, title: "Re-engagement message", message: "More than 24 hours have passed since the recipient last replied." }] },
    ] }));

    const book = JSON.parse(fs.readFileSync(path.join(dir, "data", "statuses.json"), "utf8"));
    const journal = fs.readFileSync(path.join(dir, "data", "statuses.jsonl"), "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l));
    const person = (book.people || {})[BUYER] || {};
    const m1 = (book.messages || {})["wamid.prove-1"] || {};
    const m2 = (book.messages || {})["wamid.prove-2"] || {};

    check("the inbound turn set the heard-from clock", person.lastHeardFromAt > 0, JSON.stringify(person));
    check("the read advanced the read clock to Meta's timestamp", person.lastReadAt === T2, JSON.stringify(person));
    check("the delivered clock stands at the read's moment", person.lastDeliveredAt === T2, JSON.stringify(person));
    check("the message's fate is read", m1.status === "read", JSON.stringify(m1));
    check("the duplicate delivered was not journaled again", journal.filter((j) => j.id === "wamid.prove-1").length === 2, JSON.stringify(journal));
    check("the failure kept its error code and title", m2.status === "failed" && m2.errorCode === 131047 && /Re-engagement/.test(m2.errorTitle || ""), JSON.stringify(m2));
  } catch (e) {
    check("proof ran end to end", false, e.message + " | agent log tail: " + agentLog.slice(-300));
  } finally {
    agent.kill("SIGKILL");
  }
  console.log(`\nprove-statuses: ${passed} passed, ${failed} failed`);
  process.exit(failed ? 1 : 0);
})();
