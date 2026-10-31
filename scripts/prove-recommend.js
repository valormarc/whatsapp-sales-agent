#!/usr/bin/env node
// Live proof for the buyer-side sales layer, run on the
// demo sequence: a gift ask with a budget gets a
// recommendation with a reason and an alternative; a balk
// gets a cheaper suitable option that remembers the
// budget; a buyer's photo gets the closest catalogue
// match with its differences named. Real model, dry-run
// WhatsApp sends, placeholder payment rail. The photo leg
// needs BUYER_PHOTO_MEDIA_ID (a WhatsApp media id for a
// reference photo) and a WHATSAPP_TOKEN the agent can
// download it with; without them it is skipped, not faked.
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
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "recommend-"));
fs.mkdirSync(path.join(dir, "data"), { recursive: true });
const OWNER = "233240000501", B1 = "233240000502", B2 = "233240000503";
const PRICES = { "Leather Handbag": 1800, "Beaded Clutch Bag": 1200, "Silk Scarf": 400, "Black Stiletto Heels": 950, "Runner Sneakers": 700 };
const catalogue = { merchants: {
  gift: { id: "gift", name: "Ama Serwaa Stores", shopCode: "AMAA", ownerWhatsApp: OWNER, payoutPhone: OWNER, dailyGiveawayAllowance: 400,
    products: [
      { id: "handbag", name: "Leather Handbag", description: "Structured leather handbag with a gold clasp, a proper gift.", listPrice: 1800, floorPrice: 1500, stock: 3 },
      { id: "clutch", name: "Beaded Clutch Bag", description: "Hand-beaded clutch bag for evenings and occasions.", listPrice: 1200, floorPrice: 1000, stock: 3 },
      { id: "scarf", name: "Silk Scarf", description: "Bright silk scarf, soft and light.", listPrice: 400, floorPrice: 320, stock: 3 },
      { id: "stilettos", name: "Black Stiletto Heels", description: "Black leather stiletto heels with a pointed toe.", listPrice: 950, floorPrice: 800, stock: 3 },
    ] },
  kick: { id: "kick", name: "Kick Start", shopCode: "KICK", ownerWhatsApp: OWNER, payoutPhone: OWNER, dailyGiveawayAllowance: 300,
    products: [{ id: "runners", name: "Runner Sneakers", description: "Lightweight running sneakers.", listPrice: 700, floorPrice: 560, stock: 3 }] },
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
// The seeded products named in a text, in the order named.
function namedProducts(text) {
  const hits = [];
  for (const name of Object.keys(PRICES)) {
    let i = text.indexOf(name);
    while (i >= 0) { hits.push({ name, at: i }); i = text.indexOf(name, i + 1); }
  }
  return hits.sort((a, b) => a.at - b.at).map((h) => h.name);
}
async function main() {
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
  try {
    for (let i = 0; i < 40; i++) { try { const r = await fetch(base + "/healthz"); if (r.ok) break; } catch {} await new Promise((r) => setTimeout(r, 500)); }

    console.log("1. the gift ask, with a budget");
    await say(B1, "I need a nice birthday gift for my girlfriend. My budget is around 1,500.");
    let rs = await waitFor(B1, (t) => namedProducts(t).length > 0, "the recommendation");
    let named = namedProducts(body(rs));
    check("a recommendation leads, not a catalogue list", named.length >= 1 && !body(rs).includes("Which one do you want to look at?"), body(rs));
    check("the pick respects the budget she stated", named.length >= 1 && PRICES[named[0]] <= 1500, named.join(","));
    check("an alternative is named with it", new Set(named).size >= 2, named.join(","));
    const pickPrice = PRICES[named[0]];

    console.log("2. the balk, budget and occasion remembered");
    await say(B1, "Hmm, that one is still expensive.");
    rs = await waitFor(B1, (t) => namedProducts(t).some((n) => PRICES[n] < pickPrice), "the cheaper alternative");
    named = namedProducts(body(rs));
    check("a cheaper suitable option is offered", named.some((n) => PRICES[n] < pickPrice), body(rs));

    console.log("3. the buyer's photo");
    const mediaID = process.env.BUYER_PHOTO_MEDIA_ID || "";
    if (mediaID && process.env.WHATSAPP_TOKEN) {
      await post(B1, { from: B1, id: "w" + ++seq, timestamp: String(Math.floor(Date.now() / 1000)), type: "image", image: { id: mediaID, caption: "Something like this?" } });
      rs = await waitFor(B1, (t) => t.includes("Black Stiletto Heels"), "the closest match");
      check("the closest catalogue match is named", true);
      check("the differences are stated, not dressed as exact", body(rs).includes("How it differs:"), body(rs));
    } else {
      console.log("  --  photo leg skipped: no BUYER_PHOTO_MEDIA_ID / WHATSAPP_TOKEN in env");
    }

    console.log("4. inside a shop: recommend, then a balk");
    await say(B2, "SHOP-AMAA");
    await waitFor(B2, (t) => t.includes("Leather Handbag") || t.includes("Beaded"), "the shelf");
    await say(B2, "I am looking for something for a wedding");
    rs = await waitFor(B2, (t) => namedProducts(t).length > 0, "the in-shop recommendation");
    named = namedProducts(body(rs));
    check("the shop recommends from its own shelf", named.length >= 1, body(rs));
    const shopPick = named[0];
    await say(B2, "That is too expensive");
    rs = await waitFor(B2, (t) => namedProducts(t).some((n) => PRICES[n] < PRICES[shopPick]) || /make (me|us)? ?an offer/i.test(t), "the objection answer");
    named = namedProducts(body(rs));
    check("the balk brings a cheaper alternative forward", named.some((n) => PRICES[n] < PRICES[shopPick]), body(rs));
    check("the haggle door stays open behind it", /offer|what you can|name your|your price/i.test(body(rs)), body(rs));

    console.log("5. the budget, restated, binds the next recommendation");
    await say(B2, "My budget is 500. What do you suggest?");
    rs = await waitFor(B2, (t) => namedProducts(t).length > 0, "the budget-bound recommendation");
    named = namedProducts(body(rs));
    check("the new pick respects the new budget", named.length >= 1 && PRICES[named[0]] <= 500, named.join(","));

    check("no panics in the agent log", !/panic/i.test(agentLog));
  } catch (e) {
    failed++;
    console.log("FAIL  harness: " + e.message);
    console.log(agentLog.slice(-600));
  } finally {
    agent.kill("SIGTERM");
    console.log(`\nprove-recommend: ${passed} passed, ${failed} failed  (data: ${dir})`);
    process.exit(failed ? 1 : 0);
  }
}
main();
