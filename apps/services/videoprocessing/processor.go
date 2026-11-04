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
	Clips             []Clip
	OutputResolution  string
	OutputAspectRatio string
	// AudioPath is an optional path to a background audio file.
	AudioPath *string
	// AudioVolume scales the background audio relative to the clip audio (0.0–2.0).
	// 1.0 means equal volume.
	AudioVolume float64
	// TextLayers are text overlays bound to clips by index, or to the merged
	// timeline when Absolute is set.
	TextLayers []TextLayer
	// AudioTracks are timeline-positioned audio sources mixed over the clip audio.
	// Audio past the merged video duration is cut; tracks starting at or after it
	// are ignored.
	AudioTracks []AudioTrack
	// ImageLayers are still images overlaid on the merged video, under the text.
	ImageLayers []ImageLayer
}

// ImageLayer shows a still image from StartTime to EndTime (timeline seconds). X/Y are the
// normalized centre (it may hang off the frame edges), Width the fraction of the output
// width (height keeps the image ratio), Opacity 0.05..1.
type ImageLayer struct {
	Path               string
	StartTime, EndTime float64
	X, Y, Width        float64
	Opacity            float64
}

// AudioTrack is one audio source trimmed to [TrimStart,TrimEnd) source seconds and
// placed at timeline second At.
type AudioTrack struct {
	Path      string
	TrimStart float64
	TrimEnd   float64
	At        float64
	Volume    float64 // 0..1
	// FadeIn/FadeOut ramp the volume over the first/last seconds of the trimmed span.
	FadeIn, FadeOut float64
}

// Clip is one input video with optional trim, in seconds from the source start.
type Clip struct {
	Volume    *float64 // nil preserves original audio; zero explicitly mutes.
	Path      string
	TrimStart float64 // 0 = from the start
	TrimEnd   float64 // 0 = to the full duration
	// Speed scales playback (0 = 1×, otherwise 0.25..4); the clip lasts
	// (TrimEnd-TrimStart)/Speed seconds in the output.
	Speed float64
	// FadeIn/FadeOut fade picture and clip audio from/to black, in output seconds.
	FadeIn, FadeOut float64
	Look            ClipLook
	// Transition (an xfade name) blends the previous clip into this one over
	// TransitionDuration output seconds; picture and audio overlap by that much.
	Transition         string
	TransitionDuration float64
}

// ClipLook adjusts one clip's picture; the zero value is the legacy letterboxed frame.
// Brightness/Contrast/Saturation are -1..1 and map to FFmpeg eq as brightness*0.3,
// contrast 1+c*0.5 and saturation 1+s (so -1 is greyscale). Fit "cover" fills the frame
// by cropping; Zoom (1..3, 0 = 1) scales further; PanX/PanY (-1..1) pick the visible
// part of an oversized frame, -1 being the left/top edge.
type ClipLook struct {
	Brightness float64
	Contrast   float64
	Saturation float64
	Fit        string
	Zoom       float64
	PanX       float64
	PanY       float64
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

	ScaleX, ScaleY float64 // 0 = legacy/default 1; independent text-axis scale, otherwise 0.1..5.
	FontSize       int     // px at a 1080-height reference; 0 = default (48). Scaled to output height.
	Color          string  // hex without "#"; "" = "FFFFFF"

	// Timing, in seconds relative to the clip's trimmed start. Both 0 = the whole clip.
	// When Absolute is true, ClipIdx is ignored and StartTime/EndTime are seconds on
	// the merged timeline; EndTime past the end is clipped to the output duration.
	StartTime float64
	EndTime   float64
	Absolute  bool

	// EditorStyle selects the video-editor rendering contract (requires HasXY):
	// centre-anchored block clamped inside the frame, per-line text_align, and a
	// black@0.6 drop shadow. Storyboard layers leave it false and keep their
	// original drawtext graph.
	EditorStyle bool

	// Editor-only styling. OutlineWidth is px at the 1080 reference (0 = none) in
	// OutlineColor (hex, no "#"); BoxOpacity > 0 draws a BoxColor background box;
	// Animation "fade" fades in/out and "slide" also rises into place.
	OutlineWidth int
	OutlineColor string
	BoxColor     string
	BoxOpacity   float64
	Animation    string
}
