import unittest

from transcribe import normalize_window, pause_windows, transcribe_audio

try:
    import numpy as np
except ImportError:
    np = None


class TimestampTests(unittest.TestCase):
    def test_audio_context_bounds_without_fake_word_times(self):
        rows = normalize_window({"text": " สวัสดีครับ "}, 30, 10.8)
        self.assertEqual(rows, [{"start": 30, "end": 40.8, "text": "สวัสดีครับ", "words": []}])

    def test_invalid_context_rejected(self):
        for start, duration in [(0, float("nan")), (-.1, 1), (0, 0), (0, -1)]:
            with self.assertRaises(ValueError):
                normalize_window({"text": "คำ"}, start, duration)

    def test_silent_output_has_no_segments(self):
        self.assertEqual(normalize_window({"text": ""}, 0, 3), [])


@unittest.skipIf(np is None, "NumPy required for waveform segmentation tests")
class PauseTests(unittest.TestCase):
    def tone(self, duration, amplitude=.1):
        t = np.arange(int(duration * 16000)) / 16000
        return (amplitude * np.sin(2 * np.pi * 220 * t)).astype(np.float32)

    def test_separate_actual_contexts_decoded_once_each(self):
        first, second = self.tone(8), self.tone(10)
        samples = np.concatenate((first, np.zeros(16000), second))
        windows = pause_windows(samples, np)
        self.assertEqual(len(windows), 2)
        self.assertAlmostEqual(windows[0][1] / 16000, 8.16, places=2)
        self.assertAlmostEqual(windows[1][0] / 16000, 8.84, places=2)
        self.assertGreater(windows[1][1] - windows[1][0], 6 * 16000)
        class Model:
            def __init__(self):
                self.lengths = []
            def transcribe(self, chunk, **options):
                self.lengths.append(len(chunk))
                return "บทช่วงที่ " + str(len(self.lengths))
        model = Model()
        rows = transcribe_audio(model, samples, np)
        self.assertEqual(model.lengths, [end-start for start, end in windows])
        self.assertEqual([row["text"] for row in rows], ["บทช่วงที่ 1", "บทช่วงที่ 2"])
        self.assertTrue(all(row["words"] == [] for row in rows))
        self.assertLess(rows[0]["end"], rows[1]["start"])

    def test_quiet_speech_is_preserved(self):
        for amplitude in (.1, .00005, .000001):
            samples = self.tone(9, amplitude)
            self.assertEqual(pause_windows(samples, np), [(0, len(samples))])
        # A quieter second speaker must not disappear beside a loud first one.
        samples = np.concatenate((self.tone(5), self.tone(8, .004)))
        windows = pause_windows(samples, np)
        self.assertEqual(sum(end-start for start, end in windows), len(samples))
        self.assertTrue(all(left[1] == right[0] for left, right in zip(windows, windows[1:])))

    def test_pcm16_low_voice_and_background_are_not_removed(self):
        quiet = np.round(self.tone(6, 2 / 32768) * 32768).astype(np.int16).astype(np.float32) / 32768
        self.assertGreater(float(np.max(np.abs(quiet))), 0)
        self.assertEqual(pause_windows(quiet, np), [(0, len(quiet))])
        samples = np.concatenate((self.tone(5), quiet))
        self.assertEqual(sum(end-start for start, end in pause_windows(samples, np)), len(samples))
        # Energy-only detection cannot confidently label low background as silence.
        # Retaining this uncertain span is safer than dropping a softer speaker.
        noise = np.full(3 * 16000, 1 / 32768, dtype=np.float32)
        samples = np.concatenate((self.tone(5), noise, self.tone(5)))
        windows = pause_windows(samples, np)
        self.assertEqual(sum(end-start for start, end in windows), len(samples))
        self.assertTrue(all(left[1] == right[0] for left, right in zip(windows, windows[1:])))

    def test_silence_short_gaps_and_brief_context(self):
        self.assertEqual(pause_windows(np.zeros(16000), np), [])
        samples = np.concatenate((self.tone(5), np.zeros(1600), self.tone(5)))
        self.assertEqual(pause_windows(samples, np), [(0, len(samples))])
        samples = np.concatenate((self.tone(1), np.zeros(9600), self.tone(5)))
        self.assertEqual(len(pause_windows(samples, np)), 1)

    def test_continuous_narration_offers_multiple_thirty_second_highlights(self):
        samples = self.tone(65)
        # Recorded energy valleys anchor cuts, without treating them as removed silence.
        for second in (9, 19, 29, 39, 49, 59):
            samples[second * 16000:(second * 16000) + 3200] *= .2
        windows = pause_windows(samples, np)
        self.assertGreaterEqual(len(windows), 6)
        self.assertTrue(all(end-start <= 12 * 16000 for start, end in windows))
        self.assertTrue(all(left[1] == right[0] for left, right in zip(windows, windows[1:])))
        self.assertEqual(sum(end-start for start, end in windows), len(samples))
        for _, boundary in windows[:-1]:
            self.assertLess(float(np.sqrt(np.mean(samples[boundary-400:boundary+400] ** 2))), .04)
        class Model:
            def __init__(self):
                self.chunks = []
            def transcribe(self, chunk, **options):
                self.chunks.append(chunk.copy())
                return "บริบท " + str(len(self.chunks))
        model = Model()
        rows = transcribe_audio(model, samples, np)
        self.assertEqual(len(rows), len(windows))
        for row, chunk, (start, end) in zip(rows, model.chunks, windows):
            np.testing.assert_array_equal(chunk, samples[start:end])
            self.assertAlmostEqual(row["start"], start / 16000)
            self.assertAlmostEqual(row["end"], end / 16000)
            self.assertEqual(row["words"], [])
        self.assertEqual(len(rows[:3]), 3)
        self.assertLessEqual(sum(row["end"]-row["start"] for row in rows[:3]), 30)

    def test_long_unbroken_speech_uses_bounded_nonoverlapping_windows(self):
        samples = self.tone(65)
        windows = pause_windows(samples, np)
        self.assertGreater(len(windows), 1)
        self.assertEqual(windows[0][0], 0)
        self.assertEqual(windows[-1][1], len(samples))
        self.assertTrue(all(0 < end-start <= 12 * 16000 for start, end in windows))
        self.assertTrue(all(left[1] == right[0] for left, right in zip(windows, windows[1:])))


if __name__ == "__main__":
    unittest.main()
