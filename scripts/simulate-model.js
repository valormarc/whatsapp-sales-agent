// Model-mode end-to-end proof, against a local stand-in for the Meta
// Model API (the real key is mine to supply; the wire shape is the
// one verified from Meta's official cookbook).
//
// The stand-in plays a reasonable Muse Spark: it reads the current ask
// and the private stop out of the persona system prompt it is sent,
// reads the customer's offer from the last user message, and answers
// with the JSON step the agent asked for. On the first haggle after a
// sale is on the day book, it deliberately proposes a price BELOW the
// stop. The run proves the guardrail refused it: that price never
// reaches the customer, the engine's own counter is sent instead, and
// every price the customer ever sees respects the day book's stop.
//
// Run: node scripts/simulate-model.js
//
// GO PORT ADAPTATION: this is the Node repository's
// scripts/simulate-model.js with two changes: it boots the Go binary
// (../agent, built with `go build -o agent ./cmd/agent`) instead of
// `node src/server.js`, and the two helpers it imported from the Node
// sources (getMerchant, haggleContext) are reimplemented inline below
// with the same formulas, reading the same data files. Everything
// else, the stand-in model, the scripts and the assertions, is
// unchanged, so a green run here is a parity proof against the Node
// original.
import { spawn } from 'node:child_process';
import { existsSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:http';
import { fileURLToPath } from 'node:url';

const BIN = fileURLToPath(new URL('../agent', import.meta.url));
const ROOT = fileURLToPath(new URL('..', import.meta.url));

// Inline equivalents of the Node catalogue/daybook helpers, used only
// to compute the operative stop the assertions check against.
function getMerchant(id) {
  return JSON.parse(readFileSync(new URL('../data/catalogue.json', import.meta.url), 'utf8'))
    .merchants[id];
}

function haggleContext(product, state) {
  const ratio =
    !state || !state.allowance
      ? 1
      : Math.min(1, Math.max(0, 1 - state.givenAway / state.allowance));
  const firmness = 1 - ratio;
  const floor = Math.round(
    product.floorPrice + (product.listPrice - product.floorPrice) * 0.5 * firmness,
  );
  return { floor: Math.max(product.floorPrice, floor), stepScale: 1 - 0.55 * firmness, firmness };
}

const PORT = 4200;
const MODEL_PORT = 4201;
const BASE = `http://localhost:${PORT}`;
const OUTBOX = new URL('../data/outbox.jsonl', import.meta.url);
const DAYBOOK = new URL('../data/daybook.json', import.meta.url);
const MERCHANT = '233550000999';

// Customers enter through Efua's shop link (prefilled SHOP-EFUA),
// which is what routes them to her shop on the shared number.
// The last line answers the fulfilment step: an agreement now
// waits on how the goods reach the buyer before the sale books.
const scriptA = ['SHOP-EFUA', 'Hi', 'How much is the beaded bag?', 'I can offer 500', '600', '700', '730', 'I will pick it up myself'];
const scriptB = ['SHOP-EFUA', 'How much is the beaded bag?', 'I can offer 400', '420', '450', '480', '500', '550'];

const violations = [];
let violationFired = false;

function num(text, re) {
  const m = (text || '').match(re);
  return m ? Number(m[1].replace(/,/g, '')) : null;
}

// The stand-in model: a fair trader working from the prompt it is given.
const modelServer = createServer((req, res) => {
  let body = '';
  req.on('data', (c) => (body += c));
  req.on('end', () => {
    const { messages } = JSON.parse(body || '{}');
    const system = messages.find((m) => m.role === 'system')?.content || '';
    const lastUser = [...messages].reverse().find((m) => m.role === 'user')?.content || '';
    const ask = num(system, /Current ask: GH₵([\d,]+)/);
    const stop = num(system, /private stop for this conversation is GH₵([\d,]+)/);
    const list = num(system, /IN FOCUS: [^\n]*List price GH₵([\d,]+)/);
    const sales = num(system, /THE DAY BOOK: (\d+) sale/);
    const offer = num(lastUser, /customer offers GH₵([\d,]+)/);
    const product = (lastUser.match(/for the (.+?)\. Decide/) || [])[1] || 'item';

    let proposal;
    if (sales >= 1 && !violationFired) {
      // The scripted breach: a confident, well-framed, below-stop price.
      violationFired = true;
      const bad = stop - 60;
      violations.push(bad);
      proposal = {
        action: 'counter',
        price: bad,
        message: `From GH₵${list}, I have come down to GH₵${bad} for the ${product}.`,
      };
    } else if (offer >= stop && offer >= Math.max(stop, Math.round(ask - (ask - stop) * 0.35)) - 40) {
      proposal = {
        action: 'accept',
        price: offer,
        message: `GH₵${offer} for the ${product}? Deal.`,
      };
    } else {
      const price = Math.max(stop, Math.round(ask - (ask - stop) * 0.35));
      proposal = {
        action: price <= stop ? 'final' : 'counter',
        price,
        message: `From GH₵${list}, I have come down to GH₵${price} for the ${product}.`,
      };
    }
    res.writeHead(200, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ choices: [{ message: { content: JSON.stringify(proposal) } }] }));
  });
});

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

const transcript = [];
function drainOutbox() {
  const lines = readFileSync(OUTBOX, 'utf8').trim().split('\n').filter(Boolean);
  writeFileSync(OUTBOX, '');
  return lines.map((l) => JSON.parse(l));
}

async function say(from, label, line) {
  console.log(`\n${label}: ${line}`);
  const res = await fetch(`${BASE}/webhook`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(payload(from, line)),
  });
  if (!res.ok) throw new Error(`webhook POST failed: ${res.status}`);
  await sleep(200);
  for (const m of drainOutbox()) {
    transcript.push(m);
    const who = m.to === MERCHANT ? 'agent -> MERCHANT' : 'agent';
    console.log(`${who}: ${m.body}`);
  }
}

function assert(cond, label) {
  if (!cond) throw new Error(`ASSERT FAILED: ${label}`);
  console.log(`  ok: ${label}`);
}

await new Promise((r) => modelServer.listen(MODEL_PORT, '127.0.0.1', r));

if (!existsSync(BIN)) {
  throw new Error('Go binary not built: run `go build -o agent ./cmd/agent` first');
}
const child = spawn(BIN, [], {
  cwd: ROOT,
  env: {
    ...process.env,
    PORT: `${PORT}`,
    MERCHANT_WHATSAPP: MERCHANT,
    META_MODEL_API_KEY: 'stub-key',
    META_MODEL_API_BASE: `http://127.0.0.1:${MODEL_PORT}/v1`,
  },
  stdio: 'ignore',
});

try {
  writeFileSync(OUTBOX, '');
  if (existsSync(DAYBOOK)) rmSync(DAYBOOK);
  await waitForHealth();

  console.log('\n===== CUSTOMER A (model-led, fresh day) =====');
  for (const line of scriptA) await say('233550000001', 'customer A', line);

  const dayAfterA = JSON.parse(readFileSync(DAYBOOK, 'utf8')).demo;
  const merchant = getMerchant('demo');
  const bag = merchant.products.find((p) => p.id === 'beaded-bag');
  const operativeStopB = haggleContext(bag, dayAfterA).floor;
  console.log(
    `\n----- day book after A: ${dayAfterA.sales} sale(s), GH₵${dayAfterA.givenAway} given away; operative stop for the next customer: GH₵${operativeStopB} -----`,
  );

  console.log('\n===== CUSTOMER B (model-led; the model will try one below-stop price) =====');
  for (const line of scriptB) await say('233550000002', 'customer B', line);

  console.log('\n----- proof -----');
  const toA = transcript.filter((m) => m.to === '233550000001');
  const toB = transcript.filter((m) => m.to === '233550000002');
  const dealA = toA.find((m) => m.body.includes('is yours for'));
  assert(dealA, 'customer A closed a deal');
  const dealPriceA = num(dealA.body, /yours for GH₵([\d,]+)/);
  assert(dealPriceA >= bag.floorPrice, `A's price GH₵${dealPriceA} respects the hard floor GH₵${bag.floorPrice}`);
  assert(violationFired, 'the stand-in model did attempt a below-stop proposal');
  const bad = violations[0];
  assert(
    !transcript.some((m) => m.body.includes(`GH₵${bad}`)),
    `the below-stop price GH₵${bad} never reached any customer`,
  );
  const countersB = toB
    .map(
      (m) =>
        num(m.body, /come down to GH₵([\d,]+)/) ??
        num(m.body, /last price .*GH₵([\d,]+)/),
    )
    .filter((n) => n !== null);
  assert(countersB.length >= 3, `B received ${countersB.length} counters to judge`);
  assert(
    countersB.every((n) => n >= operativeStopB),
    `every counter B saw respects the operative stop GH₵${operativeStopB} (saw: ${countersB.join(', ')})`,
  );
  assert(
    !toB.some((m) => m.body.includes('is yours for')),
    'B never closed below the stop; B was let go at the final stand',
  );
  const dayEnd = JSON.parse(readFileSync(DAYBOOK, 'utf8')).demo;
  assert(
    dayEnd.sales === 1 && dayEnd.walkAways === 1,
    `day book ends with 1 sale and 1 walk-away (got ${dayEnd.sales} and ${dayEnd.walkAways})`,
  );
  console.log('\nmodel-mode simulation complete: the model led, the guardrail held');
} finally {
  child.kill();
  modelServer.close();
}
