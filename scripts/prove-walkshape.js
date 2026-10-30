// prove-walkshape.js — live-key proof that the listing
// walk's price steps read price-shaped answers only.
//
// The failure this proves against: a seller's sentence
// about her stock ("I have 2 pairs in stock") landing at
// the last price step was read as a price, and a product
// was saved at GH₵2. The walk now refuses a sentence at a
// price step and asks again, while the shapes a seller
// actually answers in still settle. Two legs: the probe at
// the last price step, and a probe at the list price step
// followed by two spoken shapes ("It is 450 cedis", "take
// 400"). The books are read at the end: her figures stand,
// and no product anywhere carries the sentence's figure.

const { spawn } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

const PORT = 3503;
const ROOT = path.join(__dirname, "..");
const BIN = path.join(ROOT, "agent");
const BASE = `http://127.0.0.1:${PORT}`;
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "walkshape-"));
const dataDir = path.join(dir, "data");
fs.mkdirSync(dataDir, { recursive: true });

const OWNER = "233551320001";

const seedCatalogue = {
  merchants: {
    WALK: {
      id: "WALK", name: "Walk Shop", shopCode: "WALK",
      ownerWhatsApp: OWNER, dailyGiveawayAllowance: 300,
      products: [
        { id: "soap", name: "Black Soap", description: "African black soap.", listPrice: 25, floorPrice: 20, stock: 4 },
      ],
    },
  },
};
fs.writeFileSync(path.join(dataDir, "catalogue.json"), JSON.stringify(seedCatalogue));

const env = { ...process.env };
delete env.SEEV_SECRET_KEY; delete env.SEEV_PUBLIC_KEY; delete env.SEEV_WEBHOOK_SECRET;
Object.assign(env, {
  PORT: String(PORT),
  HACKATHON_AGENT_DATA_DIR: dataDir,
  WHATSAPP_DRY_RUN: "1",
  MERCHANT_WHATSAPP: OWNER,
});
const child = spawn(BIN, [], { cwd: ROOT, env, stdio: ["ignore", "pipe", "pipe"] });
let stderr = "";
child.stderr.on("data", (d) => { stderr += d; });

const outboxPath = path.join(dataDir, "outbox.jsonl");
let msgSeq = 0;
const failures = [];
const checks = [];
function check(name, ok, detail) {
  checks.push({ name, ok });
  console.log(`${ok ? "PASS" : "FAIL"} ${name}${ok ? "" : " — " + (detail || "")}`);
  if (!ok) failures.push(name);
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function outboxTo(who) {
  if (!fs.existsSync(outboxPath)) return [];
  return fs.readFileSync(outboxPath, "utf8").split("\n").filter(Boolean)
    .map((l) => { try { return JSON.parse(l); } catch { return null; } })
    .filter((r) => r && r.to === who);
}
const bodies = (rows) => rows.map((r) => r.body || "").join("\n");

function payload(from, text) {
  msgSeq += 1;
  return {
    object: "whatsapp_business_account",
    entry: [{ changes: [{ value: { messages: [{
      from, id: "wWALK-" + msgSeq + "-" + Date.now(), timestamp: "1700000000",
      type: "text", text: { body: text },
    }] } }] }],
  };
}
async function post(p) {
  const res = await fetch(BASE + "/webhook", {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(p),
  });
  if (!res.ok) throw new Error("webhook HTTP " + res.status);
}
// say sends and waits for the owner's outbox to grow and
// settle, the storm harness pattern.
async function say(from, text) {
  const before = outboxTo(from).length;
  await post(payload(from, text));
  let last = -1, stable = 0;
  for (let i = 0; i < 260; i++) {
    await sleep(600);
    const n = outboxTo(from).length;
    if (n > before && n === last) { stable++; if (stable >= 3) break; } else stable = 0;
    last = n;
  }
  return outboxTo(from).slice(before);
}
async function waitForText(who, phrase, capMs) {
  const t0 = Date.now();
  while (Date.now() - t0 < (capMs || 240000)) {
    if (bodies(outboxTo(who)).includes(phrase)) return true;
    await sleep(700);
  }
  return false;
}
async function finishWalk(name, list, last) {
  const which = await (async () => {
    const t0 = Date.now();
    while (Date.now() - t0 < 240000) {
      const all = bodies(outboxTo(OWNER));
      if (all.includes("Here is what will go live")) return "summary";
      if (all.includes("by measure")) return "measure";
      if (all.includes("How much of it is the")) return "qty";
      await sleep(700);
    }
    return "";
  })();
  if (which === "measure") await say(OWNER, "only as a whole");
  else if (which === "qty") await say(OWNER, "6 yards");
  else if (which === "") throw new Error("the walk never reached its summary");
  const ok = await waitForText(OWNER, "GH₵" + list, 60000) && await waitForText(OWNER, "GH₵" + last, 10000);
  if (!ok) throw new Error("the summary does not carry her figures");
  await say(OWNER, "yes");
}
function catalogue() {
  return JSON.parse(fs.readFileSync(path.join(dataDir, "catalogue.json"), "utf8")).merchants;
}

(async () => {
  let up = false;
  for (let i = 0; i < 60 && !up; i++) {
    try { const r = await fetch(BASE + "/healthz"); up = r.ok; } catch {}
    if (!up) await sleep(500);
  }
  if (!up) { console.error("agent never came up\n" + stderr.slice(-800)); process.exit(2); }

  try {
    // Leg 1: the probe sentence at the last price step.
    await say(OWNER, "add a product");
    if (!await waitForText(OWNER, "What is it called?", 240000)) throw new Error("no name question");
    await say(OWNER, "Shell Sandals");
    if (!await waitForText(OWNER, "Tell me about", 240000)) throw new Error("no description question");
    await say(OWNER, "Light sandals with a shell strap.");
    if (!await waitForText(OWNER, "list price", 240000)) throw new Error("no list question");
    await say(OWNER, "80");
    if (!await waitForText(OWNER, "last price", 240000)) throw new Error("no last question");
    const refusal = await say(OWNER, "I have 2 pairs in stock");
    check("the stock sentence is refused at the last price step",
      bodies(refusal).includes("did not take a last price"),
      bodies(refusal).slice(0, 160));
    check("nothing was saved by the refusal",
      !Object.values(catalogue()).some((m) => (m.products || []).some((p) => p.name === "Shell Sandals")));
    await say(OWNER, "70");
    await finishWalk("Shell Sandals", 80, 70);
    const saved1 = (catalogue().WALK.products || []).filter((p) => p.name === "Shell Sandals");
    check("the sandals stand at her figures, 80 and 70",
      saved1.length === 1 && saved1[0].listPrice === 80 && saved1[0].floorPrice === 70,
      JSON.stringify(saved1));

    // Leg 2: a probe at the list price step, then spoken shapes.
    await say(OWNER, "add a product");
    if (!await waitForText(OWNER, "Woven Basket", 1000)) { /* name comes from her answer, not the question */ }
    await say(OWNER, "Woven Basket");
    if (!await waitForText(OWNER, "Tell me about Woven Basket", 240000)) throw new Error("no description question (leg 2)");
    await say(OWNER, "Hand woven market basket.");
    const listRefusal = await say(OWNER, "I run 3 shops in Accra");
    check("the sentence is refused at the list price step",
      bodies(listRefusal).includes("did not take a price"),
      bodies(listRefusal).slice(0, 160));
    await say(OWNER, "It is 450 cedis");
    if (!await waitForText(OWNER, "last price", 240000)) throw new Error("'it is 450 cedis' did not settle the list step");
    await say(OWNER, "take 400");
    await finishWalk("Woven Basket", 450, 400);
    const saved2 = (catalogue().WALK.products || []).filter((p) => p.name === "Woven Basket");
    check("the basket stands at 450 and 400, spoken shapes intact",
      saved2.length === 1 && saved2[0].listPrice === 450 && saved2[0].floorPrice === 400,
      JSON.stringify(saved2));

    const stray = Object.values(catalogue()).some((m) => (m.products || []).some((p) => p.floorPrice === 2 || p.listPrice === 3));
    check("no product anywhere carries a sentence's figure", !stray);
    check("the agent never panicked", !stderr.includes("panic"), stderr.slice(-300));
  } catch (e) {
    check("the proof completed", false, e.message);
  } finally {
    child.kill("SIGKILL");
  }
  const failed = checks.filter((c) => !c.ok).length;
  console.log(`\nwalk shape proof: ${checks.length - failed} pass, ${failed} fail (data: ${dir})`);
  process.exit(failed ? 1 : 0);
})();
