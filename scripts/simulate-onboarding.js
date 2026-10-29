// Merchant onboarding end to end, against an EMPTY shop.
//
// Boots the Go server with HACKATHON_AGENT_DATA_DIR pointed at a
// scratch directory holding a catalogue with no products, so the
// repository's seeded catalogue is never disturbed. The merchant then
// stocks her shelf from inside the chat: the guided add-a-product
// flow (with one last price she has to correct), her allowance, and her
// own catalogue listing. A customer arrives, asks about the new
// product, haggles, and buys it above the last price she set minutes
// earlier.
//
// Run: node scripts/simulate-onboarding.js
import { spawn } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const BIN = fileURLToPath(new URL('../agent', import.meta.url));
const ROOT = fileURLToPath(new URL('..', import.meta.url));

const PORT = 4300;
const BASE = `http://localhost:${PORT}`;
const MERCHANT = '233550000999';
const CUSTOMER = '233550000001';

const DATA = mkdtempSync(join(tmpdir(), 'onboarding-sim-'));
const OUTBOX = join(DATA, 'outbox.jsonl');
const CATALOGUE = join(DATA, 'catalogue.json');
const DAYBOOK = join(DATA, 'daybook.json');

writeFileSync(
  CATALOGUE,
  JSON.stringify(
    {
      merchants: {
        demo: { id: 'demo', name: "Ama's Stall", shopCode: 'AMA', dailyGiveawayAllowance: 100, products: [] },
      },
    },
    null,
    2,
  ),
);
writeFileSync(OUTBOX, '');

let failures = 0;
function check(name, ok, detail = '') {
  console.log(`  ${ok ? 'ok' : 'FAIL'}: ${name}${ok ? '' : ` (${detail})`}`);
  if (!ok) failures++;
}

function payload(from, text) {
  return {
    object: 'whatsapp_business_account',
    entry: [
      {
        id: 'WABA_TEST',
        changes: [
          {
            field: 'messages',
            value: {
              messaging_product: 'whatsapp',
              messages: [
                {
                  from,
                  id: `wamid.test.${Math.random()}`,
                  timestamp: `${Math.floor(Date.now() / 1000)}`,
                  type: 'text',
                  text: { body: text },
                },
              ],
            },
          },
        ],
      },
    ],
  };
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function waitForHealth() {
  for (let i = 0; i < 50; i++) {
    try {
      const res = await fetch(`${BASE}/healthz`);
      if (res.ok) return;
    } catch {}
    await sleep(100);
  }
  throw new Error('server did not come up');
}

// Per-recipient drain: one inbound can answer two numbers.
const seen = {};
function drain(to) {
  const lines = readFileSync(OUTBOX, 'utf8').trim().split('\n').filter(Boolean);
  const mine = lines.map((l) => JSON.parse(l)).filter((m) => m.to === to);
  const fresh = mine.slice(seen[to] || 0);
  seen[to] = mine.length;
  return fresh.map((m) => m.body);
}

async function say(from, label, line) {
  console.log(`\n${label}: ${line}`);
  const res = await fetch(`${BASE}/webhook`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(payload(from, line)),
  });
  if (!res.ok) throw new Error(`webhook POST failed: ${res.status}`);
  await sleep(150);
  const replies = drain(from);
  for (const body of replies) console.log(`agent: ${body}`);
  return replies;
}

function last(replies) {
  return replies[replies.length - 1] || '';
}

if (!existsSync(BIN)) {
  throw new Error('Go binary not built: run `go build -o agent ./cmd/agent` first');
}
const child = spawn(BIN, [], {
  cwd: ROOT,
  env: {
    ...process.env,
    PORT: `${PORT}`,
    MERCHANT_WHATSAPP: MERCHANT,
    HACKATHON_AGENT_DATA_DIR: DATA,
    META_MODEL_API_KEY: '',
  },
  stdio: 'ignore',
});

try {
  await waitForHealth();

  console.log('\n===== THE MERCHANT STOCKS AN EMPTY SHELF =====');
  let r = await say(MERCHANT, 'merchant', 'Hello');
  check('empty shelf greets her with how to add a product', last(r).includes('add a product'));

  r = await say(MERCHANT, 'merchant', 'add a product');
  check('the flow asks for the name', last(r).includes('What is it called?'));

  r = await say(MERCHANT, 'merchant', 'Kente Cloth');
  check('the flow asks for a description', last(r).includes('Tell me about Kente Cloth'));

  r = await say(MERCHANT, 'merchant', 'Handwoven kente cloth, bright traditional patterns.');
  check('the flow asks for the list price', last(r).includes('list price'));

  r = await say(MERCHANT, 'merchant', 'GH₵600');
  check('the last price is asked for in her own words, kept from customers', last(r).includes('last price') && last(r).includes('never see it'));

  r = await say(MERCHANT, 'merchant', '650');
  check(
    'a last price above the price is refused with an explanation, not a dead end',
    last(r).includes('above your price'),
  );

  r = await say(MERCHANT, 'merchant', '480');
  check('the measure question follows the last price', last(r).includes('by measure'));

  r = await say(MERCHANT, 'merchant', 'only as a whole');
  check(
    'the summary shows name, price, last price and the giveaway margin',
    last(r).includes('Kente Cloth') && last(r).includes('GH₵600') && last(r).includes('GH₵480') && last(r).includes('GH₵120'),
  );
  const before = JSON.parse(readFileSync(CATALOGUE, 'utf8')).merchants.demo;
  check('nothing is saved before she confirms', before.products.length === 0);

  r = await say(MERCHANT, 'merchant', 'yes');
  check('her yes saves it and says it can be sold straight away', r.some((m) => m.includes('Saved') && m.includes('straight away')));
  const after = JSON.parse(readFileSync(CATALOGUE, 'utf8')).merchants.demo;
  check(
    'the product is on disk with her prices',
    after.products.length === 1 && after.products[0].listPrice === 600 && after.products[0].floorPrice === 480,
  );

  r = await say(MERCHANT, 'merchant', 'set allowance 250');
  check('she sets her daily giveaway allowance in chat', last(r).includes('GH₵250'));

  r = await say(MERCHANT, 'merchant', 'my catalogue');
  check(
    'her own listing shows the last price (merchant only)',
    last(r).includes('Kente Cloth') && last(r).includes('your last price GH₵480'),
  );

  console.log('\n===== A CUSTOMER BUYS THE NEW PRODUCT =====');
  // She arrives through the shop link, whose text carries the code.
  r = await say(CUSTOMER, 'customer', 'SHOP-AMA How much is the kente cloth?');
  check('the customer is quoted the list price she set', last(r).includes('GH₵600'));

  let dealPrice = null;
  for (const offer of ['I can offer 400', '500', '540', '560', '580']) {
    r = await say(CUSTOMER, 'customer', offer);
    for (const body of r) {
      for (const m of body.matchAll(/GH₵(\d+(?:\.\d+)?)/g)) {
        const figure = parseFloat(m[1]);
        check(`a figure shown to the customer respects her last price (${figure})`, figure >= 480 && figure <= 600, body);
      }
      const deal = body.match(/yours for GH₵(\d+(?:\.\d+)?)/);
      if (deal) dealPrice = parseFloat(deal[1]);
    }
    if (dealPrice !== null) break;
  }
  check('the customer closes a deal', dealPrice !== null);
  check('the deal is above the last price she set', dealPrice !== null && dealPrice >= 480, `${dealPrice}`);

  // The agreement alone books nothing: the shop states no
  // delivery terms, so the agent asks where she is based (the
  // fulfilment step), and her answer completes the sale.
  await say(CUSTOMER, 'customer', 'Koforidua');
  // Her place settles how the goods move; the package details
  // follow in words, and her last answer completes the sale.
  await say(CUSTOMER, 'customer', 'Akosua Mensah');
  r = await say(CUSTOMER, 'customer', 'no');
  r = await say(CUSTOMER, 'buyer', 'yes');
  check('her details complete the sale and the payment link follows', last(r).includes('Pay here'), last(r));

  const day = JSON.parse(readFileSync(DAYBOOK, 'utf8')).demo;
  check('the day book booked the sale', day.sales === 1, JSON.stringify(day));
  check('the day book runs on the allowance she set in chat', day.allowance === 250, `${day.allowance}`);
  check(
    'the giveaway came out of that allowance',
    day.givenAway === 600 - dealPrice && day.givenAway <= 250,
    `${day.givenAway}`,
  );

  const notes = drain(MERCHANT);
  check(
    'the merchant was told about the sale',
    notes.some((b) => b.includes('New order') && b.includes('Kente Cloth')),
    JSON.stringify(notes),
  );

  console.log('\n===== A SECOND PRODUCT, SOLD BY MEASURE =====');
  // She stocks a cloth priced per 6 yards and sold by the yard:
  // the measure question now takes the other branch, in two
  // answers, and the product lands on disk with its measure.
  r = await say(MERCHANT, 'merchant', 'add a product');
  check('the flow starts again for the second product', last(r).includes('What is it called?'));
  await say(MERCHANT, 'merchant', 'Ankara Cloth');
  await say(MERCHANT, 'merchant', 'Wax-print cloth, sold by the yard.');
  await say(MERCHANT, 'merchant', '300');
  r = await say(MERCHANT, 'merchant', '240');
  check('the measure question follows this last price too', last(r).includes('by measure'));
  r = await say(MERCHANT, 'merchant', 'by the yard');
  check('she is asked how much of it the price is for', last(r).includes('How much of it is the GH₵300 price for?'));
  r = await say(MERCHANT, 'merchant', '6 yards');
  check(
    'the summary states the measure in plain words',
    last(r).includes('Sold by the yard') && last(r).includes('GH₵50 per yard'),
    last(r),
  );
  r = await say(MERCHANT, 'merchant', 'yes');
  check('her yes saves the measured product', r.some((m) => m.includes('Saved')));
  const withMeasure = JSON.parse(readFileSync(CATALOGUE, 'utf8')).merchants.demo;
  const cloth = withMeasure.products.find((p) => p.name === 'Ankara Cloth');
  check(
    'the measure is on disk: unit yard, the price for 6 of them',
    cloth && cloth.unit === 'yard' && cloth.baseQty === 6,
    JSON.stringify(cloth),
  );
  r = await say(MERCHANT, 'merchant', 'my catalogue');
  check('her listing states the measure', last(r).includes('GH₵50 per yard, sold by the yard'), last(r));

  // A second customer buys a cut of it: the quantity question is
  // quoted on the scaled figures (3 yards of the GH₵300 piece is
  // GH₵150), and "Ankara 3 yards" is never read as an offer.
  const CUTTER = '233550000002';
  r = await say(CUTTER, 'cutter', 'SHOP-AMA');
  check('the second customer enters by the shop code', r.some((b) => b.includes("Welcome to Ama's Stall")), JSON.stringify(r));
  r = await say(CUTTER, 'cutter', 'Ankara 3 yards');
  check(
    'the quantity is quoted scaled, not haggled as an offer of 3',
    r.some((b) => b.includes('3 yards of the Ankara Cloth is GH₵150.')) && r.some((b) => b.includes('The full 6 yards is GH₵300.')),
    JSON.stringify(r),
  );
  let cutPrice = null;
  for (const offer of ['130', '140', '145']) {
    r = await say(CUTTER, 'cutter', offer);
    for (const body of r) {
      for (const m of body.matchAll(/GH₵(\d+(?:\.\d+)?)/g)) {
        const figure = parseFloat(m[1]);
        check(`a figure in the cut haggle respects the scaled last price (${figure})`, figure >= 120 && figure <= 150, body);
      }
      const deal = body.match(/yours for GH₵(\d+(?:\.\d+)?)/);
      if (deal) cutPrice = parseFloat(deal[1]);
    }
    if (cutPrice !== null) break;
  }
  check('the cut closes a deal', cutPrice !== null);
  check('the cut deal respects the scaled last price (GH₵120 for 3 yards)', cutPrice !== null && cutPrice >= 120, `${cutPrice}`);
  await say(CUTTER, 'cutter', 'Koforidua');
  await say(CUTTER, 'cutter', 'Akosua Mensah');
  r = await say(CUTTER, 'cutter', 'no');
  r = await say(CUTTER, 'buyer', 'yes');
  check('her details complete the cut sale with the link', last(r).includes('Pay here'), last(r));

  const orders = JSON.parse(readFileSync(join(DATA, 'orders.json'), 'utf8'));
  const cutOrder = Object.values(orders).find((o) => o.productName === 'Ankara Cloth');
  check(
    'the order records the quantity and unit',
    cutOrder && cutOrder.quantity === 3 && cutOrder.unit === 'yard',
    JSON.stringify(cutOrder),
  );
  const dayAfter = JSON.parse(readFileSync(DAYBOOK, 'utf8')).demo;
  check('the day book booked both sales', dayAfter.sales === 2, JSON.stringify(dayAfter));
  check(
    'the cut giveaway came off the scaled list, not the whole piece',
    dayAfter.givenAway === 600 - dealPrice + (150 - cutPrice),
    `${dayAfter.givenAway}`,
  );
  const cutNotes = drain(MERCHANT);
  check(
    'the merchant notice names the cut',
    cutNotes.some((b) => b.includes('*3 yards of Ankara Cloth*') && b.includes(`Sold for GH₵${cutPrice}`)),
    JSON.stringify(cutNotes),
  );

  console.log(`\nonboarding simulation ${failures === 0 ? 'complete: the merchant stocked her shelf in chat and the agent sold from it' : `FAILED with ${failures} failed check(s)`}`);
} finally {
  child.kill();
  if (failures === 0) rmSync(DATA, { recursive: true, force: true });
  else console.log(`scratch data kept for inspection: ${DATA}`);
}
process.exit(failures === 0 ? 0 : 1);
