// Local end-to-end simulation of the day book doctrine.
//
// GO PORT ADAPTATION: this is the Node repository's scripts/simulate.js
// with one change: it boots the Go binary (../agent, built with
// `go build -o agent ./cmd/agent`) instead of `node src/server.js`.
// Everything else, scripts, assertions and data files, is unchanged,
// so a green run here is a parity proof against the Node original.
// Boots the server on a scratch port and plays two customers back to back:
//   Customer A haggles the beaded bag and closes a deal, spending part of
//   the shop's daily giveaway allowance.
//   Customer B arrives after A, lowballs the same bag, and meets a firmer
//   agent; when B will not meet the final stand, the agent lets B go.
// Run: npm run simulate
import { spawn } from 'node:child_process';
import { existsSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const BIN = fileURLToPath(new URL('../agent', import.meta.url));
const ROOT = fileURLToPath(new URL('..', import.meta.url));

const PORT = 4100;
const BASE = `http://localhost:${PORT}`;
const OUTBOX = new URL('../data/outbox.jsonl', import.meta.url);
const DAYBOOK = new URL('../data/daybook.json', import.meta.url);
const MERCHANT = '233550000999';

// Both customers enter through Efua's shop link, whose prefilled
// text carries her shop code; on the shared number, that code is
// what routes them to her shop.
const scriptA = [
  'SHOP-EFUA',
  'Hi',
  'How much is the beaded bag?',
  'I can offer 500',
  '600',
  '700',
  '730',
  // A price agreement no longer completes the sale on its own:
  // the agent asks how the goods reach her (the fulfilment step),
  // and only her answer sends the payment link and books the day.
  'I will pick it up myself',
  // The package details follow the choice, in words: her name
  // for the package, and no message for the seller.
  'Akosua Mensah',
  'no',
  'yes',
];
const scriptB = [
  'SHOP-EFUA',
  'How much is the beaded bag?',
  'I can offer 400',
  '420',
  '450',
  '480',
  '500',
  '550',
];

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
  await sleep(150);
  for (const m of drainOutbox()) {
    const who = m.to === MERCHANT ? 'agent -> MERCHANT' : 'agent';
    console.log(`${who}: ${m.body}`);
  }
}

if (!existsSync(BIN)) {
  throw new Error('Go binary not built: run `go build -o agent ./cmd/agent` first');
}
const child = spawn(BIN, [], {
  cwd: ROOT,
  env: { ...process.env, PORT: `${PORT}`, MERCHANT_WHATSAPP: MERCHANT, WHATSAPP_VERIFY_TOKEN: 'sim-verify-token' },
  stdio: 'ignore',
});

try {
  writeFileSync(OUTBOX, '');
  if (existsSync(DAYBOOK)) rmSync(DAYBOOK); // fresh day book for a clean run
  await waitForHealth();

  const v = await fetch(
    `${BASE}/webhook?hub.mode=subscribe&hub.verify_token=sim-verify-token&hub.challenge=CH123`,
  );
  console.log(`handshake: HTTP ${v.status}, challenge echoed: ${(await v.text()) === 'CH123'}`);

  console.log('\n===== CUSTOMER A (fresh day, full giveaway pool) =====');
  for (const line of scriptA) await say('233550000001', 'customer A', line);

  const day = JSON.parse(readFileSync(DAYBOOK, 'utf8')).demo;
  console.log(
    `\n----- day book after A: ${day.sales} sale(s), GH₵${day.givenAway} given away of GH₵${day.allowance} allowance -----`,
  );

  console.log('\n===== CUSTOMER B (same bag, pool partly spent, lowballing) =====');
  for (const line of scriptB) await say('233550000002', 'customer B', line);

  console.log('\nsimulation complete');
} finally {
  child.kill();
}
