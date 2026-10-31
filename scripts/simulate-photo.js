// Throw-in onboarding end to end, against an EMPTY shop.
//
// Boots the Go server with HACKATHON_AGENT_DATA_DIR pointed at a
// scratch directory holding a catalogue with no products, so the
// repository's seeded catalogue is never disturbed. The merchant
// stocks her shelf the way sellers post in the selling groups:
// she throws in a photograph with a short caption carrying a size
// and an @ price, the agent reads her price and her words out of
// it, asks only for the pieces she did not give (the name, then
// her last price, which no post ever carries), and saves nothing until
// her yes. A second photo with no words at all starts the same
// walk from its first question, and she cancels it. A customer
// then arrives and buys the first product above its last price.
//
// Run: node scripts/simulate-photo.js
import { spawn } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

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

const BIN = fileURLToPath(new URL('../agent', import.meta.url));
const ROOT = fileURLToPath(new URL('..', import.meta.url));

const PORT = 4500;
const BASE = `http://localhost:${PORT}`;
const MERCHANT = '233550000999';
const CUSTOMER = '233550000001';

const DATA = mkdtempSync(join(tmpdir(), 'photo-sim-'));
const OUTBOX = join(DATA, 'outbox.jsonl');
const CATALOGUE = join(DATA, 'catalogue.json');
const DAYBOOK = join(DATA, 'daybook.json');

writeFileSync(
  CATALOGUE,
  JSON.stringify(
    {
      merchants: {
        demo: { id: 'demo', name: "Efua's Store", shopCode: 'EFUA', dailyGiveawayAllowance: 300, products: [] },
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

function payload(from, msg) {
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
                  ...msg,
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

async function post(from, label, shown, msg) {
  console.log(`\n${label}: ${shown}`);
  const res = await fetch(`${BASE}/webhook`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(payload(from, msg)),
  });
  if (!res.ok) throw new Error(`webhook POST failed: ${res.status}`);
  await sleep(150);
  const replies = drain(from);
  for (const body of replies) console.log(`agent: ${body}`);
  return replies;
}

const say = (from, label, line) => post(from, label, line, { type: 'text', text: { body: line } });
const photo = (from, label, caption) =>
  post(from, label, `[photo${caption ? `, caption: ${caption}` : ', no caption'}]`, {
    type: 'image',
    image: { id: 'media-throw-1', caption },
  });

function last(replies) {
  return replies[replies.length - 1] || '';
}

if (!existsSync(BIN)) {
  throw new Error('Go binary not built: run `go build -o agent ./cmd/agent` first');
}
const child = spawn(BIN, [], {
  cwd: ROOT,
  env: {
    WHATSAPP_DRY_RUN: '1', STALL_SANDBOX_ALLOW_MODEL: '1', WHATSAPP_APP_SECRET: '',
    ...process.env,
    PORT: `${PORT}`,
    MERCHANT_WHATSAPP: MERCHANT,
    HACKATHON_AGENT_DATA_DIR: DATA,
    META_MODEL_API_KEY: '',
    WHATSAPP_TOKEN: '',
    WHATSAPP_PHONE_NUMBER_ID: '',
  },
  stdio: 'ignore',
});

try {
  await waitForHealth();

  console.log('\n===== SHE THROWS A PRODUCT IN, GROUP STYLE =====');
  let r = await photo(MERCHANT, 'merchant', 'Size 44 Available. @250ghc');
  check('the photo is received', r[0] === 'Got your photo.', JSON.stringify(r));
  check(
    'her @ figure is read as her price, and the size is not',
    r.some((b) => b.includes('List price GH₵250')) && !r.join('\n').includes('GH₵44'),
    JSON.stringify(r),
  );
  check('her own words are kept', r.some((b) => b.includes('Size 44')), JSON.stringify(r));
  check('she is asked only for what she did not give: the name', last(r).includes('What is it called?'), last(r));

  r = await say(MERCHANT, 'merchant', 'Court Heels');
  check(
    'with the name in, the next question is her last price (the description and price already came with the photo)',
    last(r).includes('last price'),
    last(r),
  );

  r = await say(MERCHANT, 'merchant', '200');
  check('the measure question follows the last price', last(r).includes('by measure'), last(r));

  r = await say(MERCHANT, 'merchant', 'whole');
  check(
    'the summary shows the thrown-in price and her last price',
    last(r).includes('GH₵250') && last(r).includes('GH₵200'),
    last(r),
  );
  const before = JSON.parse(readFileSync(CATALOGUE, 'utf8')).merchants.demo;
  check('nothing is saved before she confirms', before.products.length === 0);

  r = await say(MERCHANT, 'merchant', 'yes');
  check('her yes saves it', r.some((m) => m.includes('Saved')), last(r));
  const after = JSON.parse(readFileSync(CATALOGUE, 'utf8')).merchants.demo;
  check(
    'the product is on disk with her prices and her words',
    after.products.length === 1 &&
      after.products[0].listPrice === 250 &&
      after.products[0].floorPrice === 200 &&
      after.products[0].description.includes('Size 44'),
    JSON.stringify(after.products),
  );

  console.log('\n===== A PHOTO WITH NO WORDS AT ALL =====');
  r = await photo(MERCHANT, 'merchant', '');
  check(
    'nothing is invented from a bare photo: the walk starts at the name',
    last(r).includes('What is it called?') && !r.join('\n').includes('List price'),
    JSON.stringify(r),
  );
  r = await say(MERCHANT, 'merchant', 'cancel');
  check('cancel drops it and saves nothing', last(r).includes('Nothing was saved'), last(r));
  const stillOne = JSON.parse(readFileSync(CATALOGUE, 'utf8')).merchants.demo;
  check('the shelf still holds only the first product', stillOne.products.length === 1);

  console.log('\n===== A CUSTOMER BUYS THE THROWN-IN PRODUCT =====');
  r = await say(CUSTOMER, 'customer', 'SHOP-EFUA Do you have the court heels?');
  check('the customer is quoted the price from her caption', r.join('\n').includes('GH₵250'), JSON.stringify(r));

  let dealPrice = null;
  for (const offer of ['200', '220', '230', '240']) {
    r = await say(CUSTOMER, 'customer', offer);
    for (const body of r) {
      for (const m of body.matchAll(/GH₵(\d+(?:\.\d+)?)/g)) {
        const figure = parseFloat(m[1]);
        check(`a figure shown to the customer respects her last price (${figure})`, figure >= 200 && figure <= 250, body);
      }
      const deal = body.match(/yours for GH₵(\d+(?:\.\d+)?)/);
      if (deal) dealPrice = parseFloat(deal[1]);
    }
    if (dealPrice !== null) break;
  }
  check('the customer closes a deal', dealPrice !== null);
  check('the deal is at or above the last price she set', dealPrice !== null && dealPrice >= 200, `${dealPrice}`);

  // The shop states no delivery terms, so the agreement asks
  // where she is based, and her answer completes the sale.
  await say(CUSTOMER, 'customer', 'Accra');
  await say(CUSTOMER, 'customer', 'Akosua Mensah');
  r = await say(CUSTOMER, 'customer', 'no');
  r = await say(CUSTOMER, 'buyer', 'yes');
  // A fresh listing is a one-off, so the sale waits at the
  // in-hand gate: the merchant's yes releases the button.
  await say(MERCHANT, 'seller', 'yes');
  r = drain(CUSTOMER);
  check('her details complete the sale and the payment link follows', last(r).includes('Pay here'), last(r));

  const day = JSON.parse(readFileSync(DAYBOOK, 'utf8')).demo;
  check('the day book booked the sale', day.sales === 1, JSON.stringify(day));

  console.log(failures === 0 ? '\nphoto simulation complete: she threw a product in and the agent sold it' : `\n${failures} check(s) failed`);
} finally {
  child.kill();
  // Wait for the child to die before clearing its data
  // away: on the kill signal the agent writes a final
  // session snapshot into that directory, and removing
  // files under a process that is still writing them
  // races (ENOTEMPTY). Three seconds is generous; the
  // wait ends the moment the child exits.
  await new Promise((resolve) => { child.once('exit', resolve); setTimeout(resolve, 3000); });
  rmSync(DATA, { recursive: true, force: true });
}
process.exit(failures === 0 ? 0 : 1);
