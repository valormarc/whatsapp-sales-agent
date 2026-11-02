# Stall's segmentation service: SAM 3 on Modal.
#
# The agent sends a seller's product photo with the
# product's name; this service finds every instance of
# that concept in the picture, cuts the product out along
# the union of the outlines, sets it on a clean
# background, and sends the composite back in the photo's
# own format. The agent does the rest — and does without,
# keeping the original photo, whenever this service
# cannot answer (see internal/agent/cutout.go).
#
# The model is Meta's SAM 3 (facebook/sam3 on Hugging
# Face, open weights, manually gated: the account behind
# the huggingface secret below must have been granted
# access on the model page first). Weights are cached in
# a volume, so they are downloaded once, not per call.
# The container scales to zero when idle: nothing is paid
# for a GPU nobody is using.
#
# Deploying and wiring this up is written down in the
# README beside this file.

import base64
import io

import modal

app = modal.App("stall-sam3")

weights = modal.Volume.from_name("stall-sam3-weights", create_if_missing=True)

image = (
    modal.Image.debian_slim(python_version="3.11")
    .pip_install(
        "torch",
        "torchvision",
        "transformers>=4.57.0",
        "accelerate",
        "pillow",
        "numpy",
        "huggingface_hub",
        "fastapi[standard]",
    )
    .env({"HF_HOME": "/weights"})
)

# The clean background every cutout is set on.
BACKGROUND = (255, 255, 255)


@app.cls(
    image=image,
    gpu="A10G",
    volumes={"/weights": weights},
    secrets=[modal.Secret.from_name("huggingface")],
    scaledown_window=120,
    timeout=1200,
)
class Segmenter:
    @modal.enter()
    def load(self):
        from transformers import Sam3Model, Sam3Processor

        self.device = "cuda"
        self.model = Sam3Model.from_pretrained("facebook/sam3").to(self.device)
        self.processor = Sam3Processor.from_pretrained("facebook/sam3")

    @modal.fastapi_endpoint(method="POST")
    def cut(self, body: dict):
        import numpy as np
        import torch
        from fastapi import HTTPException
        from PIL import Image

        prompt = (body.get("prompt") or "").strip()
        if not prompt:
            raise HTTPException(status_code=400, detail="a prompt is required")
        try:
            raw = base64.b64decode(body["image_b64"])
            photo = Image.open(io.BytesIO(raw)).convert("RGB")
        except Exception:
            raise HTTPException(status_code=400, detail="the image could not be read")

        inputs = self.processor(
            images=photo, text=prompt, return_tensors="pt"
        ).to(self.device)
        with torch.no_grad():
            outputs = self.model(**inputs)
        results = self.processor.post_process_instance_segmentation(
            outputs,
            threshold=0.5,
            mask_threshold=0.5,
            target_sizes=inputs.get("original_sizes").tolist(),
        )[0]

        masks = results["masks"]
        if masks is None or len(masks) == 0:
            # No outline for that name in this picture: the
            # agent keeps the seller's photo as taken.
            raise HTTPException(
                status_code=422, detail="no outline found for the prompt"
            )

        union = np.zeros((photo.height, photo.width), dtype=bool)
        for mask in masks:
            union |= mask.cpu().numpy().astype(bool)
        alpha = Image.fromarray((union * 255).astype("uint8"))
        canvas = Image.new("RGB", photo.size, BACKGROUND)
        canvas.paste(photo, (0, 0), alpha)

        fmt = (body.get("format") or "jpeg").lower()
        out = io.BytesIO()
        if fmt == "png":
            canvas.save(out, format="PNG")
        elif fmt == "webp":
            canvas.save(out, format="WEBP", quality=90)
        else:
            canvas.save(out, format="JPEG", quality=90)
        return {"cutout_b64": base64.b64encode(out.getvalue()).decode()}
