# Local Typhoon ASR

Typhoon ASR Real-time runs locally on CPU using the PyThaiASR ONNX backend. No API key or per-request fee is required. Whisper remains the default; provider selection is explicit and never silently falls back.

Use an isolated Python 3.11+ virtual environment. Install with `python -m pip install -r scripts/typhoon/requirements.txt`; the accompanying constraints record the exact isolated runtime package versions used for the local smoke test. Install there, not into the existing Whisper environment or global Python. Review dependency versions for the target platform before deployment.

Download the three model artifacts once, outside application requests, from [the community ONNX export](https://huggingface.co/wannaphong/typhoon-asr-realtime-onnx/tree/04af3e7b6d822cb2807bc539301669d4f84691e7) of [Typhoon ASR Real-time](https://huggingface.co/typhoon-ai/typhoon-asr-realtime):

- `encoder-fastconformer-quran-ar.onnx`
- `decoder_joint-fastconformer-quran-ar.onnx`
- `tokenizer/vocab.json`, stored as `vocab.json` in the same local model directory

The historical ONNX filenames contain `quran-ar`; the export repository identifies them as Typhoon. Pin and review revision `04af3e7b6d822cb2807bc539301669d4f84691e7`. Model weights use CC-BY-4.0: retain attribution to Typhoon/SCB 10X and the export author Wannaphong Phatthiyapha with the deployed artifacts. PyThaiASR code uses Apache-2.0. No model files or third-party code are vendored here.

```dotenv
EDITOR_ASR_PROVIDER=typhoon
TYPHOON_PYTHON=/absolute/path/typhoon-venv/bin/python
TYPHOON_RUNNER=/absolute/path/ez-gen-api/scripts/typhoon/transcribe.py
TYPHOON_MODEL_PATH=/absolute/path/typhoon-model
TYPHOON_MODEL_VERSION=hf04af3e7b6d822cb2807bc539301669d4f84691e7-pythaiasr2.3.0-ort1.23.2
```

Restart API after configuring. New jobs use a separate ASR/refinement cache namespace; existing job history is unchanged. Switch `EDITOR_ASR_PROVIDER=whisper` and restart to return to the existing runtime. Changing provider while a job is queued causes the existing signature protection to reject that old job; let active work finish before switching.

The runner reads only explicit local model paths and trusted mono 16kHz PCM WAV files. It does not download models or send audio to a cloud service. One process loads the model once for the submitted batch, with up to eight assets and 600 seconds total audio; decode contexts are bounded to 12 seconds and prefer measured quieter boundaries. This is offline batch use of a streaming-capable model, not a persistent realtime server.

Typhoon decoder token emission times are not verified acoustic word boundaries. This adapter deliberately returns `words: []`. It measures 20ms RMS frames and detects sustained quiet intervals of at least 450ms, retaining 160ms of nearby audio context on each side. Its threshold is relative to the clip's upper energy and low nonzero-energy floor; the conservative floor prevents a quiet second speaker from being mistaken for silence. Brief speech contexts under two seconds are joined to nearby context when the intervening gap is at most 1.2 seconds. Each resulting PCM span is independently transcribed by the shared model instance, so its text belongs to that actual span rather than to a longer parent transcript.

These are pause-derived contexts, not guaranteed complete sentences, detected visual shots or exact word alignment. Energy-only detection can retain breath, background noise or ambiguous low-volume audio instead of removing it. Continuous speech without a confident pause is decoded in contexts of at most 12 seconds. For a long retained region, the next boundary is selected from the quietest measured 50ms waveform interval between approximately 8 and 12 seconds, sampled every 20ms. These subdivisions keep all PCM samples in the retained region and each is independently transcribed. They provide shorter editorial choices, not a mandated shot length; planning can select adjacent contexts or longer server-issued runs when continuity is useful. A quiet energy minimum can still lie inside speech and does not establish a complete word or sentence. The upstream `typhoon-asr` package's uniformly estimated timestamp helper is not used. Where a target is shorter than an available speech context, the editor's explicitly labeled partial-context fallback still cannot guarantee complete-word cuts.

`TyphoonASR.Signature()` remains unchanged so persisted jobs retain their identity. `CacheSignature()` adds `:pause-segments-v2` for the facade's raw/refinement cache lookup; new analysis uses the new boundary generation without deleting historical transcripts. Restart the API and create a new analysis job to use the new segmentation.

Check dependencies with `TYPHOON_PYTHON scripts/typhoon/transcribe.py --check`, replacing `TYPHOON_PYTHON` with the configured executable. Real model/Thai-audio verification is required before claiming accuracy or speed improvements; successful Go/fake-process tests alone do not establish them.
