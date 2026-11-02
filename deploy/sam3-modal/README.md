# SAM 3 cutouts on Modal

`modal_app.py` is the segmentation service behind the agent's listing
photo cutouts, and it is deployed and live: the live agent and the
try-it demo both hold its address in `SAM_SEGMENT_URL`, so when a
merchant instructs the edit — her yes to the offer the agent makes on
a pictured listing, or her own words asking for it — the agent sends
the photo here and the cutout takes the photo's place. A photo is
never sent at acceptance: without her instruction it stays exactly
as she sent it.

The agent's side of the contract lives in `internal/agent/cutout.go`:
it POSTs `{image_b64, prompt, format}` and expects `{cutout_b64}`
back, the cutout composited on white in the photo's own format. The
prompt is the product's own listed name, taken from the saved
listing, never a fixed table. The agent keeps the seller's original
photo untouched beside the cutout under an `-orig` name. The call is
made only on her instruction — the offer that follows a pictured
listing (see `internal/agent/cutoutoffer.go`), or her own words —
and the listing proceeds on the original photo meanwhile: the
conversation never waits on the service. Any failure on either side
(no answer, no outline found, an implausible image back) leaves the
original photo in place: cutouts are safe to wire in, or out, with
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
taken; with it set, the agent offers the edit on pictured listings
and the cut runs only on the merchant's instruction. An optional
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
own dimensions. The WhatsApp link was proven the same day: photos
sent to the live number travelled into the cutout call and back
into their listings end to end. Those first live cuts were made
automatically, the moment each photo was accepted; the behaviour
has since changed to instructed only, and the photos cut that day
were put back to their originals, the cutouts kept beside them
under `.cutout` names. The offer, the yes, the no and the lapse
are covered by the agent's cutout offer tests.
