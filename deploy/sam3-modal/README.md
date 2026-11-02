# SAM 3 cutouts on Modal

`modal_app.py` is the segmentation service behind the agent's listing
photo cutouts. The agent's side of the contract lives in
`internal/agent/cutout.go`: it POSTs `{image_b64, prompt, format}` and
expects `{cutout_b64}` back, the cutout composited in the photo's own
format. Any failure on either side leaves the seller's original photo
in place, so the service can be wired in, or out, with config alone.

## What has to happen before the first deploy

Two authorisations belong to the account owner and cannot be done
from a shell:

1. **Modal.** The Modal client must hold a token for the workspace
   that carries the credit. Run `modal token new` and approve it in a
   browser signed in to that Modal account. `modal profile list`
   shows the workspace once it lands.
2. **Hugging Face.** The `facebook/sam3` checkpoint is manually
   gated. Request access on the model page with the Hugging Face
   account the service will use, and create a read token for it once
   access is granted.

## Deploy

Run these where the Modal API is reachable:

```sh
modal secret create huggingface HF_TOKEN=<the Hugging Face read token>
modal deploy deploy/sam3-modal/modal_app.py
```

The deploy prints the endpoint URL for the `Segmenter.cut` function.
The first call downloads the weights into the `stall-sam3-weights`
volume and is slow; later cold starts load them from the volume.

## Wire it to the agent

Put the printed URL in `app.env` for the live agent and for the
try-it demo, then restart both (delete, start, save):

```
SAM_SEGMENT_URL=<the printed endpoint URL>
```

No rebuild is needed: with the variable empty the agent never calls
out and photos stay as taken; with it set, cutouts arm themselves.
An optional `SAM_SEGMENT_KEY` is sent as a bearer token if the
endpoint is ever put behind one.

## Prove it

POST a real product photo with its name as the prompt. A `200` with a
`cutout_b64` whose composite shows the product alone on white is the
service working; a `422` means the model found no outline for that
name in that picture, and the agent will keep the original.
