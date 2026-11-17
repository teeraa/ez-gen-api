package videoprocessing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// VideoContinuationProcessor prepares a bounded reference and checks new-only output duration.
type VideoContinuationProcessor interface {
	PrepareContinuation(context.Context, []byte) ([]byte, error)
	ValidateContinuationOutput(context.Context, []byte, float64) error
}

type continuationInfo struct {
	duration      float64
	width, height int
}

func continuationProbe(c context.Context, path string) (continuationInfo, error) {
	out, e := exec.CommandContext(c, "ffprobe", "-v", "error", "-protocol_whitelist", "file", "-select_streams", "v:0", "-show_entries", "stream=width,height,duration:format=duration", "-of", "json", path).Output()
	if e != nil {
		return continuationInfo{}, errors.New("cannot inspect local continuation video")
	}
	var raw struct {
		Streams []struct {
			Width, Height int
			Duration      string
		}
		Format struct{ Duration string }
	}
	if e = json.Unmarshal(out, &raw); e != nil || len(raw.Streams) != 1 {
		return continuationInfo{}, errors.New("continuation requires one playable video stream")
	}
	duration, e := strconv.ParseFloat(raw.Streams[0].Duration, 64)
	if e != nil {
		duration, e = strconv.ParseFloat(raw.Format.Duration, 64)
	}
	if e != nil || !finite(duration) || duration <= 0 || raw.Streams[0].Width < 1 || raw.Streams[0].Height < 1 {
		return continuationInfo{}, errors.New("invalid continuation video timing")
	}
	return continuationInfo{duration: duration, width: raw.Streams[0].Width, height: raw.Streams[0].Height}, nil
}
func continuationInput(video []byte) (string, string, error) {
	if len(video) == 0 || len(video) > 300<<20 {
		return "", "", errors.New("continuation input exceeds size limit")
	}
	dir, e := os.MkdirTemp("", "director-continuation-")
	if e != nil {
		return "", "", e
	}
	path := filepath.Join(dir, "input.mp4")
	if e = os.WriteFile(path, video, 0600); e != nil {
		_ = os.RemoveAll(dir)
		return "", "", e
	}
	return dir, path, nil
}
func (p *FFmpegProcessor) PrepareContinuation(c context.Context, video []byte) ([]byte, error) {
	c, cancel := context.WithTimeout(c, 2*time.Minute)
	defer cancel()
	dir, input, e := continuationInput(video)
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	info, e := continuationProbe(c, input)
	if e != nil {
		return nil, e
	}
	if info.duration < 3 {
		return nil, errors.New("continuation source is shorter than three seconds")
	}
	ratio := float64(info.width) / float64(info.height)
	if ratio < 0.4 || ratio > 2.5 {
		return nil, errors.New("continuation source aspect ratio is unsupported")
	}
	width, height := 720, 720
	if ratio >= 1 {
		width = int(math.Round(720*ratio/2)) * 2
	} else {
		height = int(math.Round(720/ratio/2)) * 2
	}
	start := info.duration - 3
	output := filepath.Join(dir, "tail.mp4")
	vf := fmt.Sprintf("trim=start=%.9f:duration=3,setpts=PTS-STARTPTS,scale=%d:%d,setsar=1,fps=30", start, width, height)
	af := fmt.Sprintf("atrim=start=%.9f:duration=3,asetpts=PTS-STARTPTS", start)
	cmd := exec.CommandContext(c, "ffmpeg", "-nostdin", "-v", "error", "-xerror", "-protocol_whitelist", "file", "-max_alloc", "67108864", "-threads", "1", "-i", input, "-map", "0:v:0", "-map", "0:a:0?", "-vf", vf, "-af", af, "-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-pix_fmt", "yuv420p", "-threads", "1", "-filter_threads", "1", "-c:a", "aac", "-b:a", "128k", "-t", "3", "-movflags", "+faststart", "-y", output)
	if e = cmd.Run(); e != nil {
		return nil, errors.New("cannot encode local continuation tail")
	}
	checked, e := continuationProbe(c, output)
	if e != nil {
		return nil, e
	}
	if math.Abs(checked.duration-3) > 0.05 {
		return nil, errors.New("encoded continuation tail has unexpected duration")
	}
	stat, e := os.Stat(output)
	if e != nil {
		return nil, e
	}
	if stat.Size() <= 0 || stat.Size() > 200<<20 {
		return nil, errors.New("encoded continuation tail exceeds provider size limit")
	}
	return os.ReadFile(output)
}
func (p *FFmpegProcessor) ValidateContinuationOutput(c context.Context, video []byte, seconds float64) error {
	if !finite(seconds) || seconds <= 0 {
		return errors.New("invalid expected continuation duration")
	}
	c, cancel := context.WithTimeout(c, 2*time.Minute)
	defer cancel()
	dir, input, e := continuationInput(video)
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	info, e := continuationProbe(c, input)
	if e != nil {
		return e
	}
	if math.Abs(info.duration-seconds) > 0.1 {
		return errors.New("provider continuation output has unexpected duration")
	}
	// A container duration alone cannot establish that all generated frames decode successfully.
	if e = exec.CommandContext(c, "ffmpeg", "-nostdin", "-v", "error", "-xerror", "-protocol_whitelist", "file", "-max_alloc", "67108864", "-threads", "1", "-i", input, "-map", "0:v:0", "-f", "null", "-").Run(); e != nil {
		return errors.New("provider continuation video cannot be decoded")
	}
	return nil
}
