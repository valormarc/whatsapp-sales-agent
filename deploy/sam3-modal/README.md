# SAM 3 cutouts on Modal

`modal_app.py` is the segmentation service behind the agent's listing
photo cutouts, and it is deployed and live: the live agent and the
try-it demo both hold its address in `SAM_SEGMENT_URL`, so when a
photo is accepted into a listing the agent sends it here and the
cutout takes the photo's place.

The agent's side of the contract lives in `internal/agent/cutout.go`:
it POSTs `{image_b64, prompt, format}` and expects `{cutout_b64}`
back, the cutout composited on white in the photo's own format. The
prompt is the product's own listed name, taken from the saved
listing, never a fixed table. The agent keeps the seller's original
photo untouched beside the cutout under an `-orig` name. Any failure
on either side (no answer, no outline found, an implausible image
back) leaves the original photo in place, and the listing never
waits on the service: cutouts are safe to wire in, or out, with
config alone.

The model is Meta's SAM 3, the official `facebook/sam3` checkpoint,
loaded with transformers inside a Modal class on an A10G GPU. The
weights are cached in the `stall-sam3-weights` volume and the app
scales to zero when idle.

## What the deploy needs

Two authorisations belong to the account owner and cannot be done
from a shell:

1. **Modal.** The Modal client must hold a token for the workspace
   that carries the credit. Run `modal token new` and approve it in a
   browser signed in to that Modal account. `modal profile list`
   shows the workspace once it lands.
2. **Hugging Face.** The `facebook/sam3` checkpoint is manually
   gated. Request access on the model page with the Hugging Face
   account the service will use, and create a read token for it once
   access is granted. The Modal app reads that token from a Modal
   secret named `huggingface` (key `HF_TOKEN`); without the secret
   the deploy has no way to pull the weights.

Access on both sides is in place for this deployment, which is why
the service is live.

## Deploy

Run these where the Modal API is reachable:

```sh
modal secret create huggingface HF_TOKEN=<the Hugging Face read token>
modal deploy deploy/sam3-modal/modal_app.py
```

The deploy prints the endpoint URL for the `Segmenter.cut` function.
That address is deliberately kept out of this repo: the endpoint is
unauthenticated, so it lives only in the agents' env files, never in
code or docs.

## Wire it to the agent

Put the printed URL in `app.env` for the live agent and for the
try-it demo, then restart both (delete, start, save):

```
SAM_SEGMENT_URL=<the printed endpoint URL>
```

Both agents run with it set. No rebuild is needed either way: with
the variable empty the agent never calls out and photos stay as
taken; with it set, cutouts arm themselves. An optional
`SAM_SEGMENT_KEY` is sent as a bearer token if the endpoint is ever
put behind one.

## Latency

Expect a slow first call: the proof call below took 44 seconds
while the container started and the weights downloaded into the
volume. A warm call took 2 seconds. Later cold starts load the
weights from the volume rather than downloading them again.

## Prove it

POST a real product photo with its name as the prompt. A `200` with
a `cutout_b64` whose composite shows the product alone on white is
the service working; a `422` means the model found no outline for
that name in that picture, and the agent will keep the original.

This is how the live deployment was proven: the joggers photo from
a real listing (1060x1080) went in with the prompt `joggers` and
came back with all six joggers isolated on white, at the photo's
own dimensions. One link is still unproven: a photo arriving over
WhatsApp itself has not yet been run end to end into the cutout
call. The swap of a returned cutout into a listing, with the
original preserved, is covered by the agent's cutout tests.
