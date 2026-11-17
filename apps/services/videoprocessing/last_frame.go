package videoprocessing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// LastFrameExtractor is optional so merging implementations need not decode frames.
type LastFrameExtractor interface {
	ExtractLastFrame(context.Context, []byte) ([]byte, error)
}

func (p *FFmpegProcessor) ExtractLastFrame(ctx context.Context, video []byte) ([]byte, error) {
	if len(video) == 0 || len(video) > 300<<20 {
		return nil, errors.New("video size outside extraction limit")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	dir, e := os.MkdirTemp("", "director-frame-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	input, output := filepath.Join(dir, "video.mp4"), filepath.Join(dir, "last.png")
	if e = os.WriteFile(input, video, 0600); e != nil {
		return nil, e
	}
	// Decode to EOF and replace the same PNG for every frame; seeking near EOF can miss the final frame.
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-xerror", "-protocol_whitelist", "file", "-max_alloc", "67108864", "-threads", "1", "-i", input, "-map", "0:v:0", "-an", "-sn", "-dn", "-vf", "scale=w='min(iw,4096)':h='min(ih,4096)':force_original_aspect_ratio=decrease", "-vsync", "0", "-threads", "1", "-f", "image2", "-update", "1", "-y", output)
	// Discard decoder diagnostics: they may include untrusted metadata and can grow without bound.
	if e = cmd.Run(); e != nil {
		return nil, fmt.Errorf("decode final video frame: %w", e)
	}
	info, e := os.Stat(output)
	if e != nil {
		return nil, errors.New("video contains no decodable frame")
	}
	if info.Size() <= 0 || info.Size() > 32<<20 {
		return nil, errors.New("extracted frame size outside limit")
	}
	data, e := os.ReadFile(output)
	if e != nil {
		return nil, e
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 {
		return nil, errors.New("invalid extracted frame")
	}
	return data, nil
}
