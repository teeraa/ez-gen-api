package videoeditor

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
)

const MaxUploadBytes int64 = 50 << 20

var ErrInvalid = errors.New("invalid video")

type Frame struct {
	Time float64
	Data []byte
}
type Media struct {
	// Kind is "video", "audio" or "image"; audio media has no dimensions and no frames,
	// image media has no duration and one thumbnail frame.
	Kind          string
	Duration      float64
	Width, Height int
	HasAudio      bool
	MIME          string
	Frames        []Frame
}

func Available() bool {
	_, a := exec.LookPath("ffmpeg")
	_, b := exec.LookPath("ffprobe")
	return a == nil && b == nil
}

// Magic is a cheap pre-filter only; ffprobe inspection decides acceptance.
func Magic(data []byte) bool {
	if len(data) <= 12 {
		return false
	}
	switch {
	case string(data[4:8]) == "ftyp":
	case data[0] == 0x1a && data[1] == 0x45 && data[2] == 0xdf && data[3] == 0xa3:
	case string(data[0:4]) == "RIFF" && string(data[8:12]) == "WAVE":
	case string(data[0:3]) == "ID3":
	case data[0] == 0xff && data[1]&0xe0 == 0xe0:
	case string(data[1:4]) == "PNG" && data[0] == 0x89:
	case data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
	case string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
	default:
		return false
	}
	return true
}

type probeStream struct {
	CodecType   string `json:"codec_type"`
	CodecName   string `json:"codec_name"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Disposition struct {
		AttachedPic int `json:"attached_pic"`
	} `json:"disposition"`
}

var audioCodecs = map[string]bool{"aac": true, "mp3": true, "alac": true, "pcm_s16le": true, "pcm_s24le": true, "pcm_s32le": true, "pcm_f32le": true, "pcm_u8": true}

// audioOnly reports whether the container holds audio with no playable video.
// Cover art arrives as an attached-picture video stream and is ignored. Raw ADTS
// AAC (ffprobe format "aac") is not an accepted container: M4A/AAC means AAC in
// an MP4-family container.
func audioOnly(format string, streams []probeStream) bool {
	if format != "mp3" && format != "wav" && format != "mov,mp4,m4a,3gp,3g2,mj2" {
		return false
	}
	audio := false
	for _, s := range streams {
		switch s.CodecType {
		case "audio":
			audio = true
		case "video":
			if s.Disposition.AttachedPic != 1 {
				return false
			}
		}
	}
	return audio
}
func inspectAudio(ctx context.Context, path, format string, streams []probeStream, duration float64) (Media, error) {
	m := Media{Kind: "audio", Duration: duration, HasAudio: true, Frames: []Frame{}}
	switch format {
	case "mp3":
		m.MIME = "audio/mpeg"
	case "wav":
		m.MIME = "audio/wav"
	default:
		m.MIME = "audio/mp4"
	}
	// Exactly one audio stream with an allowed codec: export mixes stream 0:a:0
	// only, so additional tracks would be silently ignored.
	audios := 0
	for _, s := range streams {
		if s.CodecType == "audio" {
			audios++
			if audios > 1 || !audioCodecs[s.CodecName] {
				return Media{}, ErrInvalid
			}
		}
	}
	if audios != 1 {
		return Media{}, ErrInvalid
	}
	// Decoding the stream rejects truncated or forged files that ffprobe accepts.
	if e := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-protocol_whitelist", "file,pipe", "-i", path, "-map", "0:a:0", "-threads", "1", "-f", "null", "-").Run(); e != nil {
		return Media{}, ErrInvalid
	}
	return m, nil
}

// imageMIME maps the ffprobe still-image demuxers that are accepted to their MIME type.
var imageMIME = map[string]string{"png_pipe": "image/png", "jpeg_pipe": "image/jpeg", "webp_pipe": "image/webp"}
var imageCodecs = map[string]bool{"png": true, "mjpeg": true, "webp": true}

// inspectImage accepts one still picture up to 4096 px a side. Producing the thumbnail
// decodes it fully, which rejects truncated files and animated WebP (FFmpeg cannot decode it).
func inspectImage(ctx context.Context, path, mime string, streams []probeStream) (Media, error) {
	if len(streams) != 1 || streams[0].CodecType != "video" || !imageCodecs[streams[0].CodecName] || streams[0].Width <= 0 || streams[0].Height <= 0 || streams[0].Width > 4096 || streams[0].Height > 4096 {
		return Media{}, ErrInvalid
	}
	dir, e := os.MkdirTemp("", "video-editor-image-*")
	if e != nil {
		return Media{}, e
	}
	defer os.RemoveAll(dir)
	thumb := filepath.Join(dir, "thumb.jpg")
	if e := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-xerror", "-protocol_whitelist", "file,pipe", "-i", path, "-frames:v", "1", "-vf", "scale=160:90:force_original_aspect_ratio=decrease", "-threads", "1", "-q:v", "4", thumb).Run(); e != nil {
		return Media{}, ErrInvalid
	}
	data, e := os.ReadFile(thumb)
	if e != nil || len(data) == 0 {
		return Media{}, ErrInvalid
	}
	return Media{Kind: "image", MIME: mime, Width: streams[0].Width, Height: streams[0].Height, Frames: []Frame{{Time: 0, Data: data}}}, nil
}

func Inspect(ctx context.Context, path string) (Media, error) {
	var info struct {
		Streams []probeStream `json:"streams"`
		Format  struct {
			Duration string `json:"duration"`
			Name     string `json:"format_name"`
		} `json:"format"`
	}
	out, e := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe", "-show_entries", "stream=codec_type,codec_name,width,height:stream_disposition=attached_pic:format=duration,format_name", "-of", "json", path).Output()
	if e != nil || json.Unmarshal(out, &info) != nil {
		return Media{}, ErrInvalid
	}
	if image, ok := imageMIME[info.Format.Name]; ok {
		return inspectImage(ctx, path, image, info.Streams)
	}
	duration, e := strconv.ParseFloat(info.Format.Duration, 64)
	if e != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 || duration > 1200 {
		return Media{}, ErrInvalid
	}
	if audioOnly(info.Format.Name, info.Streams) {
		return inspectAudio(ctx, path, info.Format.Name, info.Streams, duration)
	}
	m := Media{Kind: "video", Duration: duration, MIME: "video/mp4", Frames: []Frame{}}
	if info.Format.Name == "matroska,webm" {
		m.MIME = "video/webm"
	} else if info.Format.Name != "mov,mp4,m4a,3gp,3g2,mj2" {
		return Media{}, ErrInvalid
	}
	videos := 0
	audios := 0
	for _, s := range info.Streams {
		switch s.CodecType {
		case "video":
			videos++
			if videos > 1 || s.Width <= 0 || s.Height <= 0 || s.Width > 4096 || s.Height > 4096 {
				return Media{}, ErrInvalid
			}
			switch s.CodecName {
			case "h264", "hevc", "vp8", "vp9", "av1", "mpeg4":
			default:
				return Media{}, ErrInvalid
			}
			m.Width = s.Width
			m.Height = s.Height
		case "audio":
			audios++
			if audios > 1 {
				return Media{}, ErrInvalid
			}
			switch s.CodecName {
			case "aac", "mp3", "opus", "vorbis", "pcm_s16le", "pcm_s24le", "alac":
			default:
				return Media{}, ErrInvalid
			}
			m.HasAudio = true
		default:
			return Media{}, ErrInvalid
		}
	}
	if videos != 1 {
		return Media{}, ErrInvalid
	}
	dir, e := os.MkdirTemp("", "video-editor-frames-*")
	if e != nil {
		return Media{}, e
	}
	defer os.RemoveAll(dir)
	count := int(math.Ceil(duration))
	if count < 1 {
		count = 1
	}
	if count > 24 {
		count = 24
	}
	interval := duration / float64(count)
	pattern := filepath.Join(dir, "frame-%03d.jpg")
	filter := fmt.Sprintf("fps=1/%.9f:start_time=0,scale=160:90:force_original_aspect_ratio=decrease", interval)
	if e := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-xerror", "-protocol_whitelist", "file,pipe", "-i", path, "-an", "-vf", filter, "-frames:v", strconv.Itoa(count), "-threads", "1", "-q:v", "4", pattern).Run(); e != nil {
		return Media{}, ErrInvalid
	}
	files, e := filepath.Glob(filepath.Join(dir, "frame-*.jpg"))
	if e != nil || len(files) == 0 {
		return Media{}, ErrInvalid
	}
	for i, p := range files {
		data, e := os.ReadFile(p)
		if e != nil {
			return Media{}, e
		}
		m.Frames = append(m.Frames, Frame{Time: float64(i) * interval, Data: data})
	}
	return m, nil
}
