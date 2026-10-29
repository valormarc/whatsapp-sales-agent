// Two shops on the one shared number: the real world model, proved
// end to end over HTTP.
//
// Boots the Go server against a scratch data directory holding two
// shops (Efua's Store, code EFUA, and Ama's Stall, code AMA), each
// with its own owner number, payout phone, products, prices and
// floors, so the repository's seeded catalogue is never disturbed.
// Two buyers enter by their different shop links and haggle in
// parallel; a third arrives with no code and is helped to a
// shop by discovery, but sold nothing until she picks one. The
// run asserts there is no cross talk anywhere: not in the products
// or prices a buyer sees, not in the day books, not in the orders,
// and not in which owner each merchant notice was addressed to. A
// Seev webhook then settles Ama's order, and only Ama's owner hears
// about it.
//
// Run: node scripts/simulate-two-shops.js
import { spawn } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const BIN = fileURLToPath(new URL('../agent', import.meta.url));
const ROOT = fileURLToPath(new URL('..', import.meta.url));

const PORT = 4400;
const BASE = `http://localhost:${PORT}`;

const OWNER_EFUA = '233550003001';
const OWNER_AMA = '233550003002';
const BUYER_NOCODE = '233550004101';
const BUYER_EFUA = '233550004102';
const BUYER_AMA = '233550004103';

const DATA = mkdtempSync(join(tmpdir(), 'two-shops-sim-'));
const OUTBOX = join(DATA, 'outbox.jsonl');
const CATALOGUE = join(DATA, 'catalogue.json');
const DAYBOOK = join(DATA, 'daybook.json');
const ORDERS = join(DATA, 'orders.json');

writeFileSync(
  CATALOGUE,
  JSON.stringify(
    {
      merchants: {
        efua: {
          id: 'efua',
          name: "Efua's Store",
          shopCode: 'EFUA',
          ownerWhatsApp: OWNER_EFUA,
          payoutPhone: OWNER_EFUA,
          dailyGiveawayAllowance: 300,
          products: [
            {
              id: 'beaded-bag',
              name: 'Handmade Beaded Bag',
              description: 'Hand-beaded evening bag with a gold clasp.',
              listPrice: 850,
              floorPrice: 680,
              stock: 3,
            },
          ],
        },
        ama: {
          id: 'ama',
          name: "Ama's Stall",
          shopCode: 'AMA',
          ownerWhatsApp: OWNER_AMA,
          payoutPhone: OWNER_AMA,
          dailyGiveawayAllowance: 200,
          products: [
            {
              id: 'smoked-tilapia',
              name: 'Smoked Tilapia',
              description: 'Smoked tilapia from Lake Volta, well dried.',
              listPrice: 180,
              floorPrice: 140,
              stock: 10,
            },
          ],
        },
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

// Per-recipient drain, as in the onboarding simulation: one inbound
// can answer two numbers (the buyer and her shop's owner).
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

function figures(text) {
  return [...text.matchAll(/GH₵(\d+(?:\.\d+)?)/g)].map((m) => parseFloat(m[1]));
}

if (!existsSync(BIN)) {
  throw new Error('Go binary not built: run `go build -o agent ./cmd/agent` first');
}
const child = spawn(BIN, [], {
  cwd: ROOT,
  env: {
    ...process.env,
    PORT: `${PORT}`,
    // Deliberately not either owner: any merchant notice that falls
    // back to this number instead of the selling shop's owner fails
    // the addressing checks below.
    MERCHANT_WHATSAPP: '233550009999',
    HACKATHON_AGENT_DATA_DIR: DATA,
    META_MODEL_API_KEY: '',
    SEEV_SECRET_KEY: '',
    SEEV_WEBHOOK_SECRET: '',
  },
  stdio: 'ignore',
});

try {
  await waitForHealth();

  console.log('\n===== A BUYER WITH NO SHOP CODE IS HELPED, NOT SOLD TO =====');
  let r = await say(BUYER_NOCODE, 'buyer (no code)', 'How much is the beaded bag?');
  check(
    'discovery answers with the attributed public result',
    r.join(' ').includes('EFUA has it:') && r.join(' ').includes('Price: GH₵850'),
    r.join(' '),
  );
  check('no other shop\'s goods mix into her result', !/Tilapia|GH₵180/.test(r.join(' ')), r.join(' '));
  r = await say(BUYER_NOCODE, 'buyer (no code)', 'I can offer 800');
  check('an unbound offer opens no sale', !/deal|payment link|pay here/i.test(r.join(' ')), r.join(' '));

  console.log('\n===== TWO BUYERS ENTER BY THEIR OWN SHOP LINKS =====');
  r = await say(BUYER_EFUA, 'buyer (Efua)', 'SHOP-EFUA');
  check(
    "Efua's link greets her with Efua's shop and goods only",
    r.join(' ').includes("Efua's Store") && r.join(' ').includes('Beaded Bag') && !r.join(' ').includes('Tilapia'),
    r.join(' '),
  );
  r = await say(BUYER_AMA, 'buyer (Ama)', 'shop:ama');
  check(
    "Ama's link greets her with Ama's shop and goods only",
    r.join(' ').includes("Ama's Stall") && r.join(' ').includes('Tilapia') && !r.join(' ').includes('Beaded'),
    r.join(' '),
  );

  console.log('\n===== THEY HAGGLE IN PARALLEL, EACH IN HER OWN SHOP =====');
  r = await say(BUYER_EFUA, 'buyer (Efua)', 'How much is the beaded bag?');
  check('Efua quote is the bag at GH₵850', r.join(' ').includes('GH₵850'), r.join(' '));
  r = await say(BUYER_AMA, 'buyer (Ama)', 'How much is the smoked tilapia?');
  check('Ama quote is the tilapia at GH₵180', r.join(' ').includes('GH₵180'), r.join(' '));

  r = await say(BUYER_EFUA, 'buyer (Efua)', 'I can offer 500');
  check(
    'every figure Efua\'s buyer sees sits inside Efua\'s bounds',
    figures(r.join(' ')).every((f) => f >= 680 && f <= 850),
    r.join(' '),
  );
  r = await say(BUYER_AMA, 'buyer (Ama)', 'I can offer 100');
  check(
    'every figure Ama\'s buyer sees sits inside Ama\'s bounds',
    figures(r.join(' ')).every((f) => f >= 140 && f <= 180),
    r.join(' '),
  );

  console.log('\n===== ONE OWNER CANNOT TOUCH THE OTHER SHOP =====');
  r = await say(OWNER_EFUA, 'Efua (owner)', 'change price of smoked tilapia to 1');
  check('the cross-shop command is refused', r.join(' ').includes('Nothing changed'), r.join(' '));
  const catAfter = JSON.parse(readFileSync(CATALOGUE, 'utf8'));
  check(
    "Ama's tilapia still lists at 180",
    catAfter.merchants.ama.products[0].listPrice === 180,
    JSON.stringify(catAfter.merchants.ama.products[0]),
  );

  console.log('\n===== BOTH CLOSE, EACH IN HER OWN BOOK =====');
  await say(BUYER_EFUA, 'buyer (Efua)', '650');
  r = await say(BUYER_AMA, 'buyer (Ama)', '160');
  check('Ama\'s buyer agrees the tilapia at GH₵160', r.join(' ').includes('yours for GH₵160'), r.join(' '));
  // An agreement waits on the fulfilment step: neither shop
  // states delivery terms, so each buyer is asked where she is
  // based, and her answer completes her own shop's sale.
  await say(BUYER_AMA, 'buyer (Ama)', 'Koforidua');
  await say(BUYER_AMA, 'buyer (Ama)', 'Akosua Mensah');
  r = await say(BUYER_AMA, 'buyer (Ama)', 'no');
  r = await say(BUYER_AMA, 'buyer', 'yes');
  check('Ama\'s buyer completes with the payment link', r.join(' ').includes('Pay here'), r.join(' '));
  r = await say(BUYER_EFUA, 'buyer (Efua)', '740');
  check('Efua\'s buyer agrees the bag at GH₵740', r.join(' ').includes('yours for GH₵740'), r.join(' '));
  await say(BUYER_EFUA, 'buyer (Efua)', 'Accra');
  await say(BUYER_EFUA, 'buyer (Efua)', 'Akosua Mensah');
  r = await say(BUYER_EFUA, 'buyer (Efua)', 'no');
  r = await say(BUYER_EFUA, 'buyer', 'yes');
  check('Efua\'s buyer completes with the payment link', r.join(' ').includes('Pay here'), r.join(' '));

  const day = JSON.parse(readFileSync(DAYBOOK, 'utf8'));
  check(
    "Efua's day book holds her sale alone",
    day.efua.sales === 1 && day.efua.revenue === 740 && day.efua.givenAway === 110,
    JSON.stringify(day.efua),
  );
  check(
    "Ama's day book holds her sale alone",
    day.ama.sales === 1 && day.ama.revenue === 160 && day.ama.givenAway === 20,
    JSON.stringify(day.ama),
  );

  const orders = Object.values(JSON.parse(readFileSync(ORDERS, 'utf8')));
  check('two orders were recorded', orders.length === 2, JSON.stringify(orders));
  const efuaOrder = orders.find((o) => o.merchantId === 'efua');
  const amaOrder = orders.find((o) => o.merchantId === 'ama');
  check(
    "Efua's order carries her shop and her payout phone",
    !!efuaOrder && efuaOrder.productName === 'Handmade Beaded Bag' && efuaOrder.payoutPhone === OWNER_EFUA,
    JSON.stringify(efuaOrder),
  );
  check(
    "Ama's order carries her shop and her payout phone",
    !!amaOrder && amaOrder.productName === 'Smoked Tilapia' && amaOrder.payoutPhone === OWNER_AMA,
    JSON.stringify(amaOrder),
  );
  check(
    'each order carries its own fulfilment and location',
    !!efuaOrder && efuaOrder.fulfilment === 'arranged' && efuaOrder.location === 'Accra' &&
      !!amaOrder && amaOrder.fulfilment === 'arranged' && amaOrder.location === 'Koforidua',
    JSON.stringify(orders),
  );

  const efuaNotes = drain(OWNER_EFUA).join('\n');
  check(
    'Efua was told about her own sale, and only hers',
    efuaNotes.includes('New order') && efuaNotes.includes('Beaded Bag') && !efuaNotes.includes('Tilapia'),
    efuaNotes,
  );
  const amaNotes = drain(OWNER_AMA).join('\n');
  check(
    'Ama was told about her own sale, and only hers',
    amaNotes.includes('New order') && amaNotes.includes('Tilapia') && !amaNotes.includes('Beaded'),
    amaNotes,
  );

  console.log('\n===== SETTLEMENT REACHES ONLY THE SELLING SHOP =====');
  const hook = await fetch(`${BASE}/webhooks/seev`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({
      data: { reference: 'SEEVSIM-TWO-SHOPS', status: 'completed', amount: 160 },
      meta: { order_reference: amaOrder.reference },
    }),
  });
  check('the Seev webhook answers 200', hook.status === 200, `${hook.status}`);
  await sleep(150);
  const amaSettled = drain(OWNER_AMA).join('\n');
  check(
    "Ama's owner gets the settlement figures for her sale",
    amaSettled.includes('Payment: confirmed') && amaSettled.includes('Item: Smoked Tilapia') && amaSettled.includes('GH₵157.6'),
    amaSettled,
  );
  const efuaSettled = drain(OWNER_EFUA);
  check('Efua hears nothing about Ama\'s settlement', efuaSettled.length === 0, JSON.stringify(efuaSettled));
  const settled = Object.values(JSON.parse(readFileSync(ORDERS, 'utf8'))).find((o) => o.merchantId === 'ama');
  check("Ama's order is marked paid", settled.status === 'paid', JSON.stringify(settled));

  console.log('\n===== A BUYER SWITCHES SHOPS AND STARTS CLEAN =====');
  r = await say(BUYER_EFUA, 'buyer (Efua)', 'SHOP-AMA');
  check(
    'the switch is announced by name',
    r.join(' ').includes("left Efua's Store") && r.join(' ').includes("now at Ama's Stall"),
    r.join(' '),
  );
  r = await say(BUYER_EFUA, 'buyer (now Ama)', 'How much is the beaded bag?');
  check(
    "the old shop's product is not sold in the new shop",
    !/Beaded|GH₵850/.test(r.join(' ')),
    r.join(' '),
  );

  console.log(
    `\ntwo-shop simulation ${failures === 0 ? 'complete: two shops, one number, no cross talk' : `FAILED with ${failures} failed check(s)`}`,
  );
} finally {
  child.kill();
  if (failures === 0) rmSync(DATA, { recursive: true, force: true });
  else console.log(`scratch data kept for inspection: ${DATA}`);
}
process.exit(failures === 0 ? 0 : 1);
