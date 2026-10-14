package videoprocessing

import "context"

// VideoProcessor merges video clips and applies optional audio/text post-processing.
type VideoProcessor interface {
	// Merge concatenates clips, optionally mixes background audio, and burns text
	// overlays. Returns the finished MP4 bytes.
	Merge(ctx context.Context, req MergeRequest) ([]byte, error)
}

// MergeRequest describes the merge operation.
type MergeRequest struct {
	// ClipPaths are absolute paths to clip files on disk, in presentation order.
	ClipPaths []string
	// AudioPath is an optional path to a background audio file.
	AudioPath *string
	// AudioVolume scales the background audio relative to the clip audio (0.0–2.0).
	// 1.0 means equal volume.
	AudioVolume float64
	// TextLayers are per-scene text overlays.
	TextLayers []TextLayer
}

// TextLayer describes a text burn-in that covers one full clip in the merged video.
// SceneIdx is a 0-based index into ClipPaths; the processor maps it to real timestamps
// by probing clip durations with ffprobe.
type TextLayer struct {
	SceneIdx int
	Text     string
	Position string // "top" | "center" | "bottom"
	FontSize int    // 0 = default (48px)
	Color    string // hex without #, "" = "FFFFFF"
}
