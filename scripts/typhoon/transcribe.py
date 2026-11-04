"""Offline Typhoon ONNX batch runner; only trusted worker-owned PCM paths are accepted."""
import contextlib
import importlib.metadata
import json
import math
import sys
import uuid
import wave
from pathlib import Path


MODEL_FILES = ("encoder-fastconformer-quran-ar.onnx", "decoder_joint-fastconformer-quran-ar.onnx", "vocab.json")


def normalize_window(result, offset, duration):
    text = result["text"]
    if not isinstance(text, str) or not math.isfinite(offset) or not math.isfinite(duration) or offset < 0 or duration <= 0:
        raise ValueError("Invalid transcription")
    text = text.strip()
    if not text:
        return []
    if len(text) > 10000:
        raise ValueError("Transcript limit")
    # Decoder emission times are not phonetic word boundaries. Keep the full
    # decoded audio context instead of exposing short emissions as cut anchors.
    return [{"start": offset, "end": offset + duration, "text": text, "words": []}]


def pause_windows(samples, np):
    if not len(samples) or float(np.max(np.abs(samples), initial=0)) <= 1e-7:
        return []
    frame = 320  # 20ms at the worker's fixed 16kHz sample rate.
    count = math.ceil(len(samples) / frame)
    padded = np.pad(samples, (0, count * frame - len(samples)))
    rms = np.sqrt(np.mean(padded.reshape(count, frame).astype(np.float64) ** 2, axis=1))
    # Relative energy preserves quiet speech; a low cap avoids treating softer
    # voices under a loud section as silence. This is not semantic VAD/alignment.
    positive = rms[rms > 1e-9]
    floor = float(np.percentile(positive, 10)) if len(positive) else 0
    threshold = max(1e-9, min(.003, float(np.percentile(rms, 90)) * .08, floor * .25))
    quiet = rms <= threshold
    padding = 2560  # Retain 160ms of context on each side of a detected pause.
    cursor = 0
    regions = []
    index = 0
    while index < count:
        if not quiet[index]:
            index += 1
            continue
        end = index + 1
        while end < count and quiet[end]:
            end += 1
        if (end - index) * frame >= .45 * 16000:
            left = min(len(samples), index * frame + padding)
            right = max(0, end * frame - padding)
            if index == 0:
                cursor = right
            else:
                if left > cursor:
                    regions.append((cursor, left))
                cursor = right
            if end == count:
                cursor = len(samples)
        index = end
    if cursor < len(samples):
        regions.append((cursor, len(samples)))
    # Keep brief utterances with nearby context instead of creating fragments.
    merged = []
    for start, end in regions:
        if merged and (end - start < 2 * 16000 or merged[-1][1] - merged[-1][0] < 2 * 16000) and start - merged[-1][1] <= 1.2 * 16000:
            merged[-1] = (merged[-1][0], end)
        else:
            merged.append((start, end))
    # Short decode contexts expose useful editorial choices even without digital
    # silence. Boundaries use measured energy valleys, preserve every PCM sample
    # in the retained region, and are not claimed to be word/sentence alignment.
    windows = []
    for start, stop in merged:
        while start < stop:
            end = min(start + 12 * 16000, stop)
            if end < stop:
                candidates = range(end - 4 * 16000, end - 800 + 1, 320)
                end = min(candidates, key=lambda i: float(np.mean(samples[i:i + 800] ** 2))) + 400
            windows.append((start, end))
            start = end
    return windows


def transcribe_audio(model, samples, np):
    rows = []
    for start, end in pause_windows(samples, np):
        chunk = samples[start:end]
        result = {"text": model.transcribe(chunk, sample_rate=16000, return_timestamps=False)}
        rows.extend(normalize_window(result, start / 16000, len(chunk) / 16000))
    return rows


def run():
    if sys.version_info < (3, 11):
        raise ValueError("Python 3.11 or later required")
    with contextlib.redirect_stdout(sys.stderr):
        import numpy as np
        import onnxruntime
        from pythaiasr.typhoon import FastConformerRNNT
    if importlib.metadata.version("pythaiasr") != "2.3.0" or onnxruntime.__version__ != "1.23.2":
        raise ValueError("Unsupported ASR runtime")
    if "--check" in sys.argv:
        return
    payload = json.loads(sys.stdin.buffer.read(65537))
    files = payload["files"]
    model_path = Path(payload["modelPath"])
    if not model_path.is_absolute() or not 1 <= len(files) <= 8:
        raise ValueError("Invalid model or batch")
    for name in MODEL_FILES:
        if not (model_path / name).is_file():
            raise ValueError("Missing local model")
    inputs = []
    total_samples = 0
    for item in files:
        uuid.UUID(item["assetId"])
        path = Path(item["path"])
        if not path.is_absolute() or not path.is_file() or path.stat().st_size > 20000000:
            raise ValueError("Invalid audio")
        with wave.open(str(path), "rb") as audio:
            count = audio.getnframes()
            if audio.getnchannels() != 1 or audio.getframerate() != 16000 or audio.getsampwidth() != 2 or not 0 < count <= 601 * 16000:
                raise ValueError("Invalid PCM")
            total_samples += count
            if total_samples > 601 * 16000:
                raise ValueError("Batch duration limit")
            inputs.append((item["assetId"], np.frombuffer(audio.readframes(count), dtype="<i2").astype(np.float32) / 32768))
    with contextlib.redirect_stdout(sys.stderr):
        model = FastConformerRNNT(encoder_path=str(model_path / MODEL_FILES[0]), decoder_path=str(model_path / MODEL_FILES[1]), vocab_path=str(model_path / MODEL_FILES[2]), device="cpu")
        output = []
        words = 0
        for asset_id, samples in inputs:
            rows = transcribe_audio(model, samples, np)
            words += sum(len(row["words"]) for row in rows)
            if words > 20000 or len(rows) > 2000:
                raise ValueError("Transcript limit")
            text = "\n".join(row["text"] for row in rows).strip()
            output.append({"assetId": asset_id, "language": "th", "text": text, "hasSpeech": bool(text), "segments": rows})
    encoded = json.dumps({"transcripts": output}, ensure_ascii=False, allow_nan=False)
    if len(encoded.encode()) > 8 * 1024 * 1024:
        raise ValueError("Transcript limit")
    sys.stdout.write(encoded)


if __name__ == "__main__":
    try:
        run()
    except Exception:
        sys.stderr.write("Typhoon ASR unavailable or invalid input\n")
        sys.exit(2)
