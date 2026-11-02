// Live proof for answer binding, with the real model key:
// a merchant who shares her catalogue link is met with a
// clarifying question the agent drafts on the spot, and her
// short answers to it — "Yes", then "Yes add" — must be
// read inside that question's frame. Before the fix, the
// resolving reading never saw the question: the "yes" was
// read cold against her unset allowance and landed on the
// allowance clarifier, and a product link's "New" fell out
// of the frame into a fresh greeting. The shop's allowance
// is deliberately left unset here, the bait the live
// failure bit on. Negatives are asserted strictly (the
// allowance clarifier must never appear as the destination
// of an answer owed elsewhere); the positive shape is the
// frame's own continuation — the catalogue, the question
// asked again, or the add path opening.
const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

// Sandbox hygiene: a dry-run child must never inherit live
// send or settle credentials from the operator's shell.
delete process.env.WHATSAPP_TOKEN;
delete process.env.WHATSAPP_PHONE_NUMBER_ID;
delete process.env.WHATSAPP_APP_SECRET;
delete process.env.WHATSAPP_VERIFY_TOKEN;
delete process.env.SEEV_SECRET_KEY;
delete process.env.SEEV_API_KEY;
delete process.env.SEEV_WEBHOOK_SECRET;

if (!process.env.META_MODEL_API_KEY) { console.log("META_MODEL_API_KEY required"); process.exit(1); }

const PORT = 3497;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "answerbinding-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233550009567";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    LUXE: { id: "LUXE", shopCode: "LUXE", name: "Luxe", ownerWhatsApp: OWNER,
      payoutPhone: OWNER, deliveryPaidTo: "carrier",
      products: [
        { id: "joggers", name: "joggers", description: "Thick heavyweight joggers in assorted solid colours", listPrice: 120, floorPrice: 110, stock: 4 },
      ] },
  },
}));
const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1" };
delete env.SEEV_SECRET_KEY; delete env.SEEV_API_KEY;
const agent = spawn(path.join(__dirname, "..", "agent"), [], { env, stdio: "ignore" });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let seq = 0;
async function post(message) {
  await fetch(`${BASE}/webhook`, { method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ object: "whatsapp_business_account", entry: [{ changes: [{ value: { messages: [message] } }] }] }) });
}
async function msg(from, text) { seq++; await post({ from, id: "w" + seq, type: "text", text: { body: text } }); }
function outboxTo(who) {
  const f = path.join(dir, "data", "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)).filter((r) => r.to === who);
}
async function say(from, text) {
  const before = outboxTo(from).length;
  await msg(from, text);
  let lastCount = -1, stable = 0;
  for (let i = 0; i < 60; i++) {
    await sleep(600);
    const n = outboxTo(from).length;
    if (n === lastCount) { stable++; } else { stable = 0; lastCount = n; }
    if (n > before && stable >= 4) break;
    if (i > 50 && n === before && stable >= 4) break;
  }
  await sleep(400);
  return outboxTo(from).slice(before);
}
const bodies = (rs) => rs.map((r) => r.body || "").join("\n");
const allowanceClarifier = (s) => /did you mean|do you want me to set|or to set your daily|set the daily giveaway allowance to/i.test(s);

(async () => {
  let pass = 0, fail = 0;
  const check = (name, ok) => { console.log((ok ? "PASS " : "FAIL ") + name); ok ? pass++ : fail++; };
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }

    // Scene 3, verbatim from the live journal.
    const t1 = bodies(await say(OWNER, "https://wa.me/c/233550009567"));
    console.log("link -> " + JSON.stringify(t1));
    check("the link is met with a question about the catalogue", /catalogue|link/i.test(t1) && t1.includes("?"));
    check("no allowance clarifier on the link turn", !allowanceClarifier(t1));

    const t2 = bodies(await say(OWNER, "Yes"));
    console.log("Yes -> " + JSON.stringify(t2));
    check("her yes never lands on the allowance clarifier", !allowanceClarifier(t2));
    check("her yes stays inside the catalogue frame", /catalogue|shelf|link/i.test(t2));

    const t3 = bodies(await say(OWNER, "Yes add"));
    console.log("Yes add -> " + JSON.stringify(t3));
    check("her choice never lands on the allowance clarifier", !allowanceClarifier(t3));
    check("choosing add opens the add path", /what is it called|add a new product|send me (the|a) photo|describe it/i.test(t3));

    // Scene 2's shape, after cancelling the walk the add
    // path opened: a product link, then "New".
    await say(OWNER, "cancel");
    const t4 = bodies(await say(OWNER, "https://wa.me/p/30102436722679799/233550009567"));
    console.log("product link -> " + JSON.stringify(t4));
    check("the product link is met with its question", t4.includes("?") && /add|update|new product/i.test(t4));
    const t5 = bodies(await say(OWNER, "New"));
    console.log("New -> " + JSON.stringify(t5));
    check("her New does not fall into a fresh greeting", !/Hello\. I am Stall/i.test(t5));
    check("her New starts the add path", /what is it called|add a new product/i.test(t5));
    check("no allowance clarifier anywhere in the scene", !allowanceClarifier(bodies(outboxTo(OWNER))));
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    agent.kill("SIGKILL");
    console.log(`\nanswer-binding live proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
