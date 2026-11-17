#!/usr/bin/env bash
# One-time local Typhoon ASR setup for development (macOS/Linux).
# Creates ./typhoon-venv, installs the pinned runtime, downloads the pinned model
# revision into ./models/typhoon-asr and runs the runner's dependency check.
# Re-running is safe: existing files are verified and kept.
set -euo pipefail

cd "$(dirname "$0")/../.."
ROOT="$(pwd)"
REVISION="04af3e7b6d822cb2807bc539301669d4f84691e7"
REPO="https://huggingface.co/wannaphong/typhoon-asr-realtime-onnx/resolve/${REVISION}"
VENV="${ROOT}/typhoon-venv"
MODEL_DIR="${ROOT}/models/typhoon-asr"

PY="${PYTHON:-}"
if [ -z "$PY" ]; then
  for candidate in python3.13 python3.12 python3.11 python3; do
    if command -v "$candidate" >/dev/null 2>&1 && "$candidate" -c 'import sys; sys.exit(0 if sys.version_info >= (3, 11) else 1)'; then
      PY="$candidate"; break
    fi
  done
fi
if [ -z "$PY" ]; then
  echo "Python 3.11+ not found. Install it (e.g. brew install python@3.11) or set PYTHON=/path/to/python3.11" >&2
  exit 1
fi
echo "Using $("$PY" --version) at $(command -v "$PY")"

if [ ! -x "${VENV}/bin/python" ]; then
  "$PY" -m venv "$VENV"
fi
"${VENV}/bin/python" -m pip install --upgrade pip >/dev/null
"${VENV}/bin/python" -m pip install -r scripts/typhoon/requirements.txt

mkdir -p "$MODEL_DIR"
download() {
  local remote="$1" local_name="$2"
  if [ -s "${MODEL_DIR}/${local_name}" ]; then
    echo "Keeping existing ${local_name}"
    return
  fi
  echo "Downloading ${remote} ..."
  curl -fL --retry 3 --progress-bar -o "${MODEL_DIR}/${local_name}.part" "${REPO}/${remote}"
  mv "${MODEL_DIR}/${local_name}.part" "${MODEL_DIR}/${local_name}"
}
download "encoder-fastconformer-quran-ar.onnx" "encoder-fastconformer-quran-ar.onnx"
download "decoder_joint-fastconformer-quran-ar.onnx" "decoder_joint-fastconformer-quran-ar.onnx"
download "tokenizer/vocab.json" "vocab.json"

"${VENV}/bin/python" scripts/typhoon/transcribe.py --check
echo
echo "Typhoon ASR ready."
echo "  TYPHOON_PYTHON=${VENV}/bin/python"
echo "  TYPHOON_MODEL_PATH=${MODEL_DIR}"
echo "Model weights: Typhoon ASR Real-time (SCB 10X), ONNX export by Wannaphong Phatthiyaphaibun, CC-BY-4.0."
echo "Restart the API to pick up the new ASR provider."
