# Local transcription runtime

Use Python 3.11 or later on a supported glibc Linux or macOS host. No global Python installation is changed by the application. The default API Alpine image has no Python/CT2 runtime and will report transcription unavailable; do not enable the feature until a compatible runtime is installed.

```sh
python3.11 -m venv /opt/violet/whisper-venv
/opt/violet/whisper-venv/bin/python -m pip install -r scripts/whisper/requirements.txt
/opt/violet/whisper-venv/bin/python scripts/whisper/transcribe.py --check
```

The constraints are the exact dependency versions scanned against OSV on 2026-10-27 in the isolated Python 3.11 test runtime. Review and audit dependency upgrades for the deployment platform; do not reuse old Python 3.9 transitive versions. `faster-whisper` is pinned to 1.2.1 (MIT).

Model download is an explicit administrative setup step, never a side effect of a user job or capability request. Choose a multilingual model appropriate to available CPU/GPU and evaluate the actual Thai footage. `tiny` is useful for smoke tests, not an accuracy guarantee. `small` is a reasonable first CPU evaluation candidate; larger models need more resources.

```python
from huggingface_hub import snapshot_download
snapshot_download("Systran/faster-whisper-small", revision="<reviewed-model-commit>", local_dir="/opt/violet/models/whisper-small")
```

Configure server-only environment variables (never browser public variables):

```text
EDITOR_AI_ENABLED=true
GEMINI_API_KEY=<set through secret storage>
GEMINI_MODEL=gemini-3.5-flash-lite
GEMINI_MODEL_VERSION=<reviewed-deployment-cache-generation>
WHISPER_PYTHON=/opt/violet/whisper-venv/bin/python
WHISPER_RUNNER=/opt/violet/ez-gen-api/scripts/whisper/transcribe.py
WHISPER_MODEL_PATH=/opt/violet/models/whisper-small
WHISPER_MODEL_VERSION=<exact-model-commit>
WHISPER_DEVICE=cpu
WHISPER_COMPUTE_TYPE=int8
WHISPER_LANGUAGE=
```

An empty language asks Whisper to detect language. Set `th` for Thai-only deployments if evaluated. Both model-version values are required cache boundaries; bump them for model/runtime/prompt generation changes. The model path must be an absolute local directory with `model.bin`. The runner imports dependencies for `--check` without loading model weights; it uses local-files-only loading during transcription. Restart the API after installing/changing the runtime because the capability probe is memoized.

The Go worker extracts trusted mono 16kHz PCM audio from owned videos. The CLI accepts JSON on stdin and returns bounded JSON on stdout. Paths are never accepted from the browser. It runs argv directly without a shell, uses VAD and timestamp output, and its process group is killed on cancellation on Unix. Do not treat Thai token timestamps as lexical word segmentation. No raw audio is sent to Gemini: only sampled JPEGs and compact text are cloud input.

The selected Gemini account governs quota and billing. The application caps attempts and does not fall back to a different paid model, but cannot guarantee a billing-enabled key is free. Free-service data terms can permit provider use of content; the user must acknowledge the cloud notice and use suitable non-confidential material. Never place a key in logs or source.
