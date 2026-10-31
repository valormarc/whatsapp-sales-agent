// Live proof for the listing announcements, with the real
// model: a seller lists a product on an empty shop, and the
// save hands her a Status caption and a broadcast, drafted
// by the thinking, each carrying her price and her shop
// link, neither carrying her private last price.
const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

// Sandbox hygiene: a dry-run child must never inherit live
// send or settle credentials from the operator's shell. The
// agent's sandbox exception is bound to a send-incapable
// process, so the live WhatsApp and Seev credentials are
// blanked here, before any child env is built from this one.
delete process.env.WHATSAPP_TOKEN;
delete process.env.WHATSAPP_PHONE_NUMBER_ID;
delete process.env.WHATSAPP_APP_SECRET;
delete process.env.WHATSAPP_VERIFY_TOKEN;
delete process.env.SEEV_SECRET_KEY;
delete process.env.SEEV_API_KEY;
delete process.env.SEEV_WEBHOOK_SECRET;

const PORT = 3473;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "announce-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000999";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    EFUA: { id: "EFUA", shopCode: "EFUA", name: "Efua's Store", ownerWhatsApp: OWNER,
      payoutPhone: OWNER, dailyGiveawayAllowance: 300, products: [] },
  },
}));
const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"),
  WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1", MERCHANT_WHATSAPP: OWNER, AGENT_WHATSAPP_NUMBER: "233209450786" };
delete env.SEEV_SECRET_KEY; delete env.SEEV_API_KEY;
const agent = spawn(path.join(__dirname, "..", "agent"), [], { env, stdio: "ignore" });

let seq = 0;
async function msg(from, text) {
  seq++;
  await fetch(`${BASE}/webhook`, { method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ object: "whatsapp_business_account", entry: [{ changes: [{ value: { messages: [{ from, id: "w" + seq, type: "text", text: { body: text } }] } }] }] }) });
}
function outboxTo(who) {
  const f = path.join(dir, "data", "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)).filter((r) => r.to === who);
}
async function waitFor(fn, label, timeoutMs = 60000) {
  const start = Date.now();
  for (;;) {
    if (fn()) return true;
    if (Date.now() - start > timeoutMs) throw new Error("timeout: " + label);
    await new Promise((r) => setTimeout(r, 700));
  }
}
const told = (sub) => () => outboxTo(OWNER).some((r) => (r.body || "").includes(sub));

(async () => {
  let pass = 0, fail = 0;
  const check = (name, ok) => { console.log((ok ? "PASS " : "FAIL ") + name); ok ? pass++ : fail++; };
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await new Promise((r) => setTimeout(r, 400)); }

    let answeredUpTo = 0;
    const drive = async (answers, doneLabel, timeoutMs = 90000) => {
      const start = Date.now();
      for (;;) {
        const rows = outboxTo(OWNER);
        if (rows.some((r) => (r.body || "").includes(doneLabel))) return;
        if (Date.now() - start > timeoutMs) throw new Error("timeout driving to " + doneLabel);
        if (rows.length > answeredUpTo) {
          answeredUpTo = rows.length;
          const last = (rows[rows.length - 1].body || "").toLowerCase();
          for (const [frag, reply] of answers) {
            if (last.includes(frag)) { await msg(OWNER, reply); break; }
          }
        }
        await new Promise((r) => setTimeout(r, 800));
      }
    };
    await msg(OWNER, "add a product");
    await drive([
      ["called", "Ankle Strap Heels"],
      ["tell me about", "Pointed ankle-strap heels in gold and black mesh, very fine for occasions."],
      ["list price", "GH₵120"],
      ["last price", "100"],
      ["by measure", "only as a whole"],
      ["shall i add", "yes"],
    ], "To forward to your contacts and groups:", 120000);

    const all = outboxTo(OWNER).map((r) => r.body || "");
    const statusIdx = all.findIndex((b) => b.includes("For your Status, copy this:"));
    const broadcastIdx = all.findIndex((b) => b.includes("To forward to your contacts and groups:"));
    check("she is handed a Status caption and a broadcast", statusIdx >= 0 && broadcastIdx >= 0);
    const status = all[statusIdx] || "";
    const broadcast = all[broadcastIdx] || "";
    console.log("STATUS DRAFT:\n" + status + "\nBROADCAST DRAFT:\n" + broadcast);
    const link = "https://wa.me/233209450786?text=SHOP-EFUA";
    check("both carry her price and her shop link",
      status.includes("GH₵120") && status.includes(link) && broadcast.includes("GH₵120") && broadcast.includes(link));
    check("neither carries her last price", !status.includes("GH₵100") && !broadcast.includes("GH₵100"));
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    agent.kill("SIGKILL");
    console.log(`\nannounce live proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
