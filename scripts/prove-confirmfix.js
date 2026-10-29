// Live proof for the confirm-step fixes, with the real
// model, against the exact failure of a live phone run: a
// buyer who had the wrong name on her package quoted the
// agent's own name question with the right name, three
// times, and the sale completed with the wrong name.
//
//   S1. At the confirm step she quotes the name question
//       with a new name: it lands, the order is read back
//       with it, and the completed order carries it.
//   S2. At the confirm step she states the correction in
//       plain words, quoting nothing: the reading names the
//       field and the value, and it lands the same way.
//   S3. An unbound buyer with results standing says a bare
//       "Hello": the reply holds the pick question up again
//       instead of starting over.
//   S4. One result shown, and she points at it ("I will
//       take this"): that is a pick of that result.
const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

if (!process.env.META_MODEL_API_KEY) { console.log("META_MODEL_API_KEY required"); process.exit(1); }

const PORT = 3499;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "confirmfix-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000999";
const AGENTNUM = "233209450786";
const B1 = "233551119101", B2 = "233551119102", B3 = "233551119103", B4 = "233551119104";
fs.writeFileSync(path.join(dir, "data", "catalogue.json"), JSON.stringify({
  merchants: {
    KOFI: { id: "KOFI", shopCode: "KOFI", name: "Kofi Shoes", ownerWhatsApp: OWNER,
      payoutPhone: OWNER, dailyGiveawayAllowance: 300, deliveryFee: 25,
      deliveryNote: "Sent by bus to your station",
      products: [
        { id: "heels", name: "Ankle Strap Heels", description: "Pointed ankle-strap heels in mesh", listPrice: 120, floorPrice: 100, stock: 3, sizes: ["37", "38", "39", "40", "41", "42"] },
        { id: "bag", name: "Leather Bag", description: "A structured leather handbag", listPrice: 850, floorPrice: 800, stock: 2 },
      ] },
  },
}));
const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1", AGENT_WHATSAPP_NUMBER: AGENTNUM };
delete env.SEEV_SECRET_KEY; delete env.SEEV_API_KEY;
const agent = spawn(path.join(__dirname, "..", "agent"), [], { env, stdio: "ignore" });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let seq = 0;
async function msg(from, text, context) {
  seq++;
  const m = { from, id: "w" + seq, type: "text", text: { body: text } };
  if (context) m.context = context;
  await fetch(`${BASE}/webhook`, { method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ object: "whatsapp_business_account", entry: [{ changes: [{ value: { messages: [m] } }] }] }) });
}
function outboxTo(who) {
  const f = path.join(dir, "data", "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)).filter((r) => r.to === who);
}
function orders() { const f = path.join(dir, "data", "orders.json"); return fs.existsSync(f) ? Object.values(JSON.parse(fs.readFileSync(f, "utf8"))) : []; }
async function turn(from, text, context) {
  const before = outboxTo(from).length;
  await msg(from, text, context);
  let last = -1, stable = 0;
  for (let i = 0; i < 70; i++) {
    await sleep(600);
    const n = outboxTo(from).length;
    if (n === last) stable++; else { stable = 0; last = n; }
    if (n > before && stable >= 3) break;
    if (i > 45 && n === before && stable >= 3) break;
  }
  await sleep(400);
  return outboxTo(from).slice(before);
}
const bodies = (recs) => recs.map((r) => r.body || "").join("\n");
async function walkToConfirm(buyer, name) {
  await turn(buyer, "SHOP-KOFI");
  await turn(buyer, "Leather Bag");
  await turn(buyer, "850");
  await turn(buyer, "I will pick it up myself");
  const q = outboxTo(buyer).filter((r) => (r.body || "").includes("What name should go on the package?")).pop();
  await turn(buyer, name);
  await turn(buyer, "no");
  return q && q.id;
}

(async () => {
  let pass = 0, fail = 0;
  const check = (name, ok) => { console.log((ok ? "PASS " : "FAIL ") + name); ok ? pass++ : fail++; };
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }

    // S1: the quoted name correction at confirm.
    const nameQ = await walkToConfirm(B1, "Akosua Mensah");
    check("the name question carries a quotable id", !!nameQ);
    const fixed = await turn(B1, "Kojo Tsey", { id: nameQ, from: AGENTNUM });
    check("the quoted correction is read back with the new name", bodies(fixed).includes("Kojo Tsey"));
    check("the old name is gone from the read-back", !bodies(fixed).includes("Akosua Mensah"));
    await turn(B1, "yes");
    const o1 = orders().find((o) => o.from === B1);
    check("the completed order carries the corrected name", !!o1 && o1.packageName === "Kojo Tsey");

    // S2: the same correction stated plainly, no quote.
    await walkToConfirm(B2, "Akosua Mensah");
    const fixed2 = await turn(B2, "No, it should say Efua Nyarko");
    check("a plainly stated correction lands by meaning", bodies(fixed2).includes("Efua Nyarko") && !bodies(fixed2).includes("Akosua Mensah"));

    // S3: a bare greeting with results standing.
    await turn(B3, "Do you have laptops?");
    const greet = await turn(B3, "Hello");
    check("the greeting holds the shown results up again", bodies(greet).includes("Ankle Strap Heels") || bodies(greet).includes("Leather Bag"));

    // S4: pointing at the one shown result is a pick.
    await turn(B4, "Do you sell handbags?");
    const pick = await turn(B4, "I will take this");
    check("pointing at the shown bag takes her to it", bodies(pick).includes("Price: GH₵850"));
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    agent.kill("SIGKILL");
    console.log(`\nconfirm-fix live proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
