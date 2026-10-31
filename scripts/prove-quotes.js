// Live proof for quoted replies, with the real model: a
// reply that quotes an old message must be understood
// against THAT message — what it said, and who sent it —
// however far the conversation has wandered since.
//
//   A. An unbound buyer is shown the heels, then the bag;
//      she quotes the OLD heels bubble and says "Ok": she
//      is taken into the shop on the heels, and her next
//      plain offer is heard on the heels.
//   B. A buyer inside the shop views the heels, wanders to
//      the bag, then quotes the heels bubble with an offer:
//      the haggle that answers is the heels'.
//   C. A buyer quotes her OWN earlier "I am in Accra" and
//      corrects it to Kumasi: the fulfilment that follows
//      names Kumasi, never Accra.
//   D. Two buyers claim direct payments; the merchant
//      quotes the SECOND claim's alert and says yes: that
//      claim settles, the older one stays open.
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

if (!process.env.META_MODEL_API_KEY) { console.log("META_MODEL_API_KEY required"); process.exit(1); }

const PORT = 3493;
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "quotes-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000999";
const AGENTNUM = "233209450786";
const A = "233551119001", B = "233551119002", C = "233551119003", D1 = "233551119004", D2 = "233551119005";
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
const env = { ...process.env, PORT: String(PORT), HACKATHON_AGENT_DATA_DIR: path.join(dir, "data"), WHATSAPP_DRY_RUN: "1", STALL_SANDBOX_ALLOW_MODEL: "1", AGENT_WHATSAPP_NUMBER: AGENTNUM };
delete env.SEEV_SECRET_KEY; delete env.SEEV_API_KEY;
const agent = spawn(path.join(__dirname, "..", "agent"), [], { env, stdio: "ignore" });

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let seq = 0;
async function post(message) {
  await fetch(`${BASE}/webhook`, { method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ object: "whatsapp_business_account", entry: [{ changes: [{ value: { messages: [message] } }] }] }) });
}
async function msg(from, text, context) {
  seq++;
  const m = { from, id: "w" + seq, type: "text", text: { body: text } };
  if (context) m.context = context;
  await post(m);
  return m.id;
}
async function img(from, mediaID) {
  seq++;
  await post({ from, id: "w" + seq, type: "image", image: { id: mediaID, caption: "" } });
}
function outboxTo(who) {
  const f = path.join(dir, "data", "outbox.jsonl");
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)).filter((r) => r.to === who);
}
function orders() { const f = path.join(dir, "data", "orders.json"); return fs.existsSync(f) ? Object.values(JSON.parse(fs.readFileSync(f, "utf8"))) : []; }
// Send one message and wait until its replies settle;
// returns only the records that turn produced for her.
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
const quoteRecID = (who, price) => {
  const rec = outboxTo(who).filter((r) => (r.body || "").includes("Price: GH₵" + price)).pop();
  return rec && rec.id;
};

(async () => {
  let pass = 0, fail = 0;
  const check = (name, ok) => { console.log((ok ? "PASS " : "FAIL ") + name); ok ? pass++ : fail++; };
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(`${BASE}/healthz`); if (r.ok) break; } catch {} await sleep(400); }

    // A. Discovery, wander, quoted Ok.
    await turn(A, "Where can I buy heels?");
    const heelsQuoteA = quoteRecID(A, "120");
    check("the heels presentation carries a quotable id", !!heelsQuoteA);
    await turn(A, "Do you have bags?");
    const quoted = await turn(A, "Ok", { id: heelsQuoteA, from: AGENTNUM });
    check("the quoted Ok takes her into the shop on the heels", bodies(quoted).includes("Ankle Strap Heels"));
    const offer = await turn(A, "110");
    // The haggle's own words vary (the model drafts them), so
    // the property is the figures: from the heels' GH₵120,
    // never the bag's GH₵850, and no bag dealing.
    check("her next plain offer is heard on the heels, not the bag", (bodies(offer).includes("GH₵120") || bodies(offer).includes("Heels")) && !bodies(offer).includes("GH₵850") && !bodies(offer).includes("Leather Bag"));
    check("the heels offer moves at heels figures", /GH₵1(0\d|1\d|20)/.test(bodies(offer)));

    // B. In-shop wander, quoted offer.
    await turn(B, "SHOP-KOFI");
    await turn(B, "Ankle Strap Heels");
    const heelsQuoteB = quoteRecID(B, "120");
    await turn(B, "Leather Bag");
    const qo = await turn(B, "Can you do it for 110?", { id: heelsQuoteB, from: AGENTNUM });
    check("the quoted offer is answered on the heels", (bodies(qo).includes("GH₵120") || bodies(qo).includes("Heels")) && !bodies(qo).includes("GH₵850") && !bodies(qo).includes("Leather Bag"));
    check("the quoted haggle works at heels figures", /GH₵1(0\d|1\d|20)/.test(bodies(qo)));

    // C. Quoting her own words to correct them.
    await turn(C, "SHOP-KOFI");
    const ownID = await msg(C, "I am in Accra");
    await sleep(1500);
    await turn(C, "Leather Bag");
    await turn(C, "How much is the Leather Bag?");
    await turn(C, "Change that, I am in Kumasi", { id: ownID, from: C });
    const deal = await turn(C, "850");
    check("the fulfilment names the corrected place, Kumasi", bodies(deal).includes("Kumasi"));
    check("the old place, Accra, is nowhere in the fulfilment", !bodies(deal).includes("Accra"));

    // D. The buyer quotes her pay button and says she paid:
    // the report attaches to THAT order, the agent checks the
    // payment service itself, and no merchant is asked
    // anything. (There are no claims any more: payment is
    // link-only and Stall verifies it.)
    await turn(D1, "SHOP-KOFI");
    await turn(D1, "Ankle Strap Heels");
    await turn(D1, "120");
    await turn(D1, "I will pick it up myself");
    await turn(D1, "40"); // the heels' size, when asked
    await turn(D1, "Akosua Mensah");
    await turn(D1, "no");
    await turn(D1, "yes");
    const payRec = outboxTo(D1).filter((r) => r.kind === "cta_url").pop();
    const payID = payRec && payRec.id;
    check("the pay button carries a quotable id", !!payID);
    const ownerBefore = outboxTo(OWNER).length;
    const paidReply = await turn(D1, "Paid", { id: payID, from: AGENTNUM });
    // This harness runs on the placeholder payment rail (no
    // Seev keys), so the check's honest answer is that there
    // is no session to check — and it names the QUOTED order,
    // which is what proves the report landed on it.
    check("the quoted Paid is answered about the quoted order itself", bodies(paidReply).includes("KOFI-0001") && bodies(paidReply).includes("no payment session to check"));
    check("the merchant is told nothing about the report", outboxTo(OWNER).length === ownerBefore);
    const os_ = orders();
    check("the order stays open on her word alone", os_.length === 1 && os_[0].status === "pending");
  } catch (e) {
    console.log("ERROR " + e.message); fail++;
  } finally {
    agent.kill("SIGKILL");
    console.log(`\nquotes live proof: ${pass} pass, ${fail} fail`);
    process.exit(fail ? 1 : 0);
  }
})();
