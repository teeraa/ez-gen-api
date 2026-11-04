"""Local-only batch runner. Media paths are supplied by the trusted Go worker."""
import json
import os
import sys
import uuid
import wave
from pathlib import Path


def run():
    if sys.version_info < (3, 11):
        raise ValueError("Python 3.11 or later required")
    from faster_whisper import WhisperModel
    if "--check" in sys.argv:
        return
    payload = json.loads(sys.stdin.buffer.read(65537))
    files = payload["files"]
    model_path = Path(payload["modelPath"])
    if not model_path.is_absolute() or not (model_path / "model.bin").is_file() or not 1 <= len(files) <= 8:
        raise ValueError("Invalid local model or batch")
    if payload["device"] not in ("cpu", "cuda", "auto") or payload["computeType"] not in ("int8", "int8_float32", "float32", "float16", "int8_float16", "auto"):
        raise ValueError("Unsupported runtime")
    for item in files:
        uuid.UUID(item["assetId"])
        path = Path(item["path"])
        if not path.is_absolute() or not path.is_file() or path.stat().st_size > 20000000:
            raise ValueError("Invalid audio")
        with wave.open(str(path), "rb") as audio:
            if audio.getnchannels() != 1 or audio.getframerate() != 16000 or audio.getsampwidth() != 2 or audio.getnframes() / 16000 > 601:
                raise ValueError("Invalid PCM")
    model = WhisperModel(str(model_path), device=payload["device"], compute_type=payload["computeType"], local_files_only=True, cpu_threads=min(4, os.cpu_count() or 2))
    output = []
    count = 0
    for item in files:
        segments, info = model.transcribe(item["path"], language=payload.get("language") or None, word_timestamps=True, vad_filter=True, beam_size=5)
        rows = []
        for segment in segments:
            words = [{"start": w.start, "end": w.end, "text": w.word} for w in (segment.words or [])]
            count += len(words)
            if count > 20000:
                raise ValueError("Transcript limit")
            rows.append({"start": segment.start, "end": segment.end, "text": segment.text, "words": words})
        text = "".join(row["text"] for row in rows).strip()
        output.append({"assetId": item["assetId"], "language": info.language, "text": text, "hasSpeech": bool(text), "segments": rows})
    encoded = json.dumps({"transcripts": output}, ensure_ascii=False, allow_nan=False)
    if len(encoded.encode()) > 8 * 1024 * 1024:
        raise ValueError("Transcript limit")
    sys.stdout.write(encoded)


if __name__ == "__main__":
    try:
        run()
    except Exception:
        sys.stderr.write("ASR unavailable or invalid input\n")
        sys.exit(2)
