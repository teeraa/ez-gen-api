package videoprocessing

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// Delivery loudness of a finished film: -14 LUFS integrated, the level online platforms play
// video at, with the true peak kept at -1.5 dBTP (below the usual -1 dBTP, headroom for the
// AAC encode) and a loudness range a film can breathe in.
const (
	MasterLoudness = -14.0
	MasterTruePeak = -1.5
	MasterRange    = 11.0
)

// masterDuck lowers the music bed under speech. The key is the film's own audio limited to the
// speech band, so the room tone under dubbed shots (about -50 dBFS) never triggers it while
// dialogue (lines are treated to -20 LUFS) pulls the music down by about 12 dB; 20 ms attack,
// 400 ms release so the music comes back between lines without pumping.
const masterDuck = "sidechaincompress=threshold=0.02:ratio=20:attack=20:release=400:knee=2"

// masterFormat brings audio to the mix format.
const masterFormat = "aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo"

// MasterAudio writes dst: the film at src with its video stream copied and its audio mixed with
// the optional music bed (ducked under dialogue) and normalized to MasterLoudness in two passes
// (measure, then a linear gain where the true peak allows it). A silent mix is left at its level.
func MasterAudio(ctx context.Context, src, dst string, music *AudioTrack) error {
	info, e := probe(ctx, src)
	if e != nil {
		return fmt.Errorf("videoprocessing: master: %w", e)
	}
	if !info.Audio || info.Width <= 0 {
		return fmt.Errorf("videoprocessing: master: film needs a video and an audio stream")
	}
	graph, inputs, e := masterGraph(music)
	if e != nil {
		return e
	}
	inputs = append([]string{"-i", src}, inputs...)

	measure := append(append([]string{"-hide_banner", "-nostats"}, inputs...), "-filter_complex",
		graph+fmt.Sprintf(";[mix]loudnorm=I=%.1f:TP=%.1f:LRA=%.1f:print_format=json[out]", MasterLoudness, MasterTruePeak, MasterRange),
		"-map", "[out]", "-f", "null", "-")
	out, e := exec.CommandContext(ctx, "ffmpeg", measure...).CombinedOutput()
	if e != nil {
		return fmt.Errorf("videoprocessing: master measure: %w: %s", e, lastLines(string(out), 6))
	}
	m, ok := parseLoudnorm(string(out))
	final := "[mix]anull[out]"
	if ok {
		final = fmt.Sprintf("[mix]loudnorm=I=%.1f:TP=%.1f:LRA=%.1f:measured_I=%.2f:measured_TP=%.2f:measured_LRA=%.2f:measured_thresh=%.2f:offset=%.2f:linear=true,%s[out]",
			MasterLoudness, MasterTruePeak, MasterRange, m.I, m.TP, m.LRA, m.Thresh, m.Offset, masterFormat)
	}
	render := append(append([]string{"-y", "-v", "error"}, inputs...), "-filter_complex", graph+";"+final,
		"-map", "0:v:0", "-map", "[out]", "-c:v", "copy", "-c:a", "aac", "-b:a", "192k", "-ar", "48000",
		"-t", fmt.Sprintf("%.6f", info.Duration), "-movflags", "+faststart", dst)
	if out, e = exec.CommandContext(ctx, "ffmpeg", render...).CombinedOutput(); e != nil {
		return fmt.Errorf("videoprocessing: master: %w: %s", e, lastLines(string(out), 6))
	}
	done, e := probe(ctx, dst)
	if e != nil || !done.Audio || done.Width != info.Width || math.Abs(done.Duration-info.Duration) > 0.15 {
		return fmt.Errorf("videoprocessing: master: invalid output")
	}
	return nil
}

// masterGraph builds the mix up to the [mix] label and the extra inputs it reads.
func masterGraph(music *AudioTrack) (string, []string, error) {
	film := "[0:a:0]" + masterFormat
	if music == nil {
		return film + "[mix]", nil, nil
	}
	t := *music
	if t.Path == "" || !finite(t.TrimStart) || !finite(t.TrimEnd) || !finite(t.At) || t.TrimStart < 0 || t.TrimEnd <= t.TrimStart || t.At < 0 || !finite(t.Volume) || t.Volume < 0 || t.Volume > 1 {
		return "", nil, fmt.Errorf("videoprocessing: master: invalid music timing or volume")
	}
	delay := int64(math.Round(t.At * 48000))
	parts := []string{
		film + ",asplit=2[film][k0]",
		"[k0]highpass=f=300,lowpass=f=3400[key]",
		fmt.Sprintf("[1:a:0]atrim=start=%.6f:end=%.6f,asetpts=PTS-STARTPTS,%s,volume=%.6f%s,adelay=delays=%dS:all=1[m0]",
			t.TrimStart, t.TrimEnd, masterFormat, t.Volume, audioFadeFilters(t.FadeIn, t.FadeOut, t.TrimEnd-t.TrimStart), delay),
		"[m0][key]" + masterDuck + "[mus]",
		"[film][mus]amix=inputs=2:duration=first:normalize=0[mix]",
	}
	return strings.Join(parts, ";"), []string{"-i", t.Path}, nil
}

// loudnormStats are the first-pass measurements loudnorm prints.
type loudnormStats struct{ I, TP, LRA, Thresh, Offset float64 }

// parseLoudnorm reads the JSON block loudnorm prints at the end of a measuring pass. It reports
// false for silence (-inf) or anything it cannot read, so the mix is then left as it is.
func parseLoudnorm(out string) (loudnormStats, bool) {
	start, end := strings.LastIndex(out, "{"), strings.LastIndex(out, "}")
	if start < 0 || end < start {
		return loudnormStats{}, false
	}
	var raw struct {
		I      string `json:"input_i"`
		TP     string `json:"input_tp"`
		LRA    string `json:"input_lra"`
		Thresh string `json:"input_thresh"`
		Offset string `json:"target_offset"`
	}
	if json.Unmarshal([]byte(out[start:end+1]), &raw) != nil {
		return loudnormStats{}, false
	}
	var s loudnormStats
	for _, f := range []struct {
		text string
		dst  *float64
	}{{raw.I, &s.I}, {raw.TP, &s.TP}, {raw.LRA, &s.LRA}, {raw.Thresh, &s.Thresh}, {raw.Offset, &s.Offset}} {
		v, e := strconv.ParseFloat(strings.TrimSpace(f.text), 64)
		if e != nil || !finite(v) {
			return loudnormStats{}, false
		}
		*f.dst = v
	}
	return s, true
}
