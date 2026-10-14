package videoprocessing

import "context"

// VideoProcessor merges video clips and applies optional audio/text post-processing.
type VideoProcessor interface {
	// Merge trims and concatenates clips, optionally mixes background audio, and burns
	// text overlays. Returns the finished MP4 bytes.
	Merge(ctx context.Context, req MergeRequest) ([]byte, error)
}

// MergeRequest describes the merge operation.
type MergeRequest struct {
	// Clips are the inputs in presentation order.
	Clips []Clip
	// AudioPath is an optional path to a background audio file.
	AudioPath *string
	// AudioVolume scales the background audio relative to the clip audio (0.0–2.0).
	// 1.0 means equal volume.
	AudioVolume float64
	// TextLayers are text overlays bound to clips by index.
	TextLayers []TextLayer
}

// Clip is one input video with optional trim, in seconds from the source start.
type Clip struct {
	Path      string
	TrimStart float64 // 0 = from the start
	TrimEnd   float64 // 0 = to the full duration
}

// TextLayer describes a text burn-in over one clip in the merged video.
type TextLayer struct {
	// ClipIdx is a 0-based index into Clips.
	ClipIdx int
	Text    string

	// Placement. When HasXY is true, X/Y are normalized (0..1) frame coordinates with the
	// text box centre as the anchor; Align shifts the anchor horizontally. When HasXY is
	// false, Position ("top"|"center"|"bottom") is used with horizontal centring.
	HasXY    bool
	X, Y     float64
	Position string
	Align    string // "left" | "center" | "right"; default center

	FontSize int    // px at a 1080-height reference; 0 = default (48). Scaled to output height.
	Color    string // hex without "#"; "" = "FFFFFF"

	// Timing, in seconds relative to the clip's trimmed start. Both 0 = the whole clip.
	StartTime float64
	EndTime   float64
}
