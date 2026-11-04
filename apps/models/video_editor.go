package models

import (
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"time"
)

// VideoEditorSegment is one span of a video asset on the main track. Start/End are
// source seconds; Speed (0 = 1×) plays it faster or slower, so the span takes
// (End-Start)/Speed timeline seconds. FadeIn/FadeOut are timeline seconds of a fade
// from/to black (picture and the clip's own audio).
type VideoEditorSegment struct {
	ID      string  `json:"id"`
	AssetID string  `json:"assetId"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Volume  float64 `json:"volume"`
	Speed   float64 `json:"speed,omitempty"`
	FadeIn  float64 `json:"fadeIn,omitempty"`
	FadeOut float64 `json:"fadeOut,omitempty"`
	VideoEditorClipLook
	VideoEditorTransition
}

// VideoEditorTransition blends the previous segment into this one over
// TransitionDuration seconds (0.1..2, at most half of either segment), so this segment
// starts that much earlier on the timeline. The first segment never has one.
type VideoEditorTransition struct {
	Transition         string  `json:"transition,omitempty"`
	TransitionDuration float64 `json:"transitionDuration,omitempty"`
}

// VideoEditorTimelineLength totals segments after speed, minus transition overlaps.
func VideoEditorTimelineLength(segments []VideoEditorSegment) float64 {
	total := 0.0
	for i, s := range segments {
		total += s.TimelineLength()
		if i > 0 {
			total -= s.TransitionDuration
		}
	}
	return total
}

// VideoEditorClipLook is the picture adjustment of a segment; zero values mean unchanged.
// Brightness/Contrast/Saturation are -1..1 adjustments (see videoprocessing.ClipLook for
// the FFmpeg mapping). Fit "cover" fills the frame by cropping (default letterboxes);
// Zoom 1..3 (0 = 1) enlarges further; PanX/PanY -1..1 choose which part stays visible.
type VideoEditorClipLook struct {
	Brightness float64 `json:"brightness,omitempty"`
	Contrast   float64 `json:"contrast,omitempty"`
	Saturation float64 `json:"saturation,omitempty"`
	Fit        string  `json:"fit,omitempty"`
	Zoom       float64 `json:"zoom,omitempty"`
	PanX       float64 `json:"panX,omitempty"`
	PanY       float64 `json:"panY,omitempty"`
}

// SegmentSpeed treats an absent speed as normal playback.
func SegmentSpeed(speed float64) float64 {
	if speed == 0 {
		return 1
	}
	return speed
}

// TimelineLength is how long the segment lasts on the timeline after its speed.
func (s VideoEditorSegment) TimelineLength() float64 {
	return (s.End - s.Start) / SegmentSpeed(s.Speed)
}

// VideoEditorAudioClip places a span of an audio asset (or the sound of a video
// asset, when detached) on the timeline; At is in timeline seconds while Start/End
// are source seconds. FadeIn/FadeOut are seconds of volume ramp at each end.
type VideoEditorAudioClip struct {
	OriginJobID string  `json:"originJobId,omitempty"`
	ID          string  `json:"id"`
	AssetID     string  `json:"assetId"`
	At          float64 `json:"at"`
	Start       float64 `json:"start"`
	End         float64 `json:"end"`
	Volume      float64 `json:"volume"`
	FadeIn      float64 `json:"fadeIn,omitempty"`
	FadeOut     float64 `json:"fadeOut,omitempty"`
}

// VideoEditorTextLayer is a timeline-absolute burned-in caption. X/Y are the
// normalized box centre; FontSize is pixels at a 1080-pixel-high reference.
type VideoEditorTextLayer struct {
	OriginJobID string   `json:"originJobId,omitempty"`
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	Start       float64  `json:"start"`
	End         float64  `json:"end"`
	X           float64  `json:"x"`
	Y           float64  `json:"y"`
	FontSize    int      `json:"fontSize"`
	ScaleX      *float64 `json:"scaleX,omitempty"`
	ScaleY      *float64 `json:"scaleY,omitempty"`
	Color       string   `json:"color"`
	Align       string   `json:"align"`
	// Optional style: an outline OutlineWidth px (at 1080) wide, a background box of
	// BoxColor at BoxOpacity (0 = none), and an entrance/exit Animation ("fade" or "slide").
	OutlineColor string  `json:"outlineColor,omitempty"`
	OutlineWidth int     `json:"outlineWidth,omitempty"`
	BoxColor     string  `json:"boxColor,omitempty"`
	BoxOpacity   float64 `json:"boxOpacity,omitempty"`
	Animation    string  `json:"animation,omitempty"`
}

// VideoEditorImageLayer shows a still image (sticker, logo) from Start to End timeline
// seconds. X/Y are the normalized centre, Width the fraction of the frame width (height
// follows the image ratio), Opacity 0.05..1.
type VideoEditorImageLayer struct {
	ID      string  `json:"id"`
	AssetID string  `json:"assetId"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Width   float64 `json:"width"`
	Opacity float64 `json:"opacity"`
}
type VideoEditorFrame struct {
	Time float64 `json:"time"`
	Key  string  `json:"key"`
}
type VideoEditorProject struct {
	Base
	UserID      uuid.UUID `gorm:"type:uuid;not null"`
	RequestID   uuid.UUID `gorm:"type:uuid;not null"`
	RequestHash string
	Title       string
	AspectRatio string
	Revision    int
	Segments    datatypes.JSON `gorm:"type:jsonb"`
	AudioClips  datatypes.JSON `gorm:"type:jsonb"`
	TextLayers  datatypes.JSON `gorm:"type:jsonb"`
	// ImageLayers exists only after migration 028, so GORM may read and update it but never
	// insert it ("->;<-:update"): project creation must work before the migration. A plain
	// read-only "->" would also silently drop it from Updates maps.
	ImageLayers datatypes.JSON `gorm:"type:jsonb;->;<-:update"`
}

func (VideoEditorProject) TableName() string { return "video_editor_projects" }

type VideoEditorAsset struct {
	Base
	UserID         uuid.UUID `gorm:"type:uuid;not null"`
	ProjectID      uuid.UUID `gorm:"type:uuid;not null"`
	ClientUploadID uuid.UUID `gorm:"type:uuid;not null"`
	ContentHash    string
	Name           string
	Kind           string `gorm:"default:video"`
	Status         string
	Duration       float64
	Width          int
	Height         int
	HasAudio       bool
	SizeBytes      int64
	SourceKey      string
	Frames         datatypes.JSON `gorm:"type:jsonb"`
	AttemptToken   uuid.UUID      `gorm:"type:uuid"`
	LeaseUntil     *time.Time
	FailureCode    string
	FailureMessage string
}

func (VideoEditorAsset) TableName() string { return "video_editor_assets" }

type VideoEditorSnapshotClip struct {
	AssetID string  `json:"assetId"`
	Key     string  `json:"key"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Volume  float64 `json:"volume"`
	Speed   float64 `json:"speed,omitempty"`
	FadeIn  float64 `json:"fadeIn,omitempty"`
	FadeOut float64 `json:"fadeOut,omitempty"`
	VideoEditorClipLook
	VideoEditorTransition
}
type VideoEditorSnapshotAudio struct {
	AssetID string  `json:"assetId"`
	Key     string  `json:"key"`
	At      float64 `json:"at"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Volume  float64 `json:"volume"`
	FadeIn  float64 `json:"fadeIn,omitempty"`
	FadeOut float64 `json:"fadeOut,omitempty"`
}
type VideoEditorSnapshotImage struct {
	AssetID string  `json:"assetId"`
	Key     string  `json:"key"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Width   float64 `json:"width"`
	Opacity float64 `json:"opacity"`
}
type VideoEditorSnapshot struct {
	ImageLayers []VideoEditorSnapshotImage `json:"imageLayers,omitempty"`
	Clips       []VideoEditorSnapshotClip  `json:"clips"`
	AudioClips  []VideoEditorSnapshotAudio `json:"audioClips"`
	TextLayers  []VideoEditorTextLayer     `json:"textLayers"`
	AspectRatio string                     `json:"aspectRatio"`
	Resolution  string                     `json:"resolution"`
}
type VideoEditorExport struct {
	Base
	UserID         uuid.UUID `gorm:"type:uuid;not null"`
	ProjectID      uuid.UUID `gorm:"type:uuid;not null"`
	RequestID      uuid.UUID `gorm:"type:uuid;not null"`
	RequestHash    string
	Revision       int
	Status         string
	Resolution     string
	AspectRatio    string
	Duration       float64
	Snapshot       datatypes.JSON `gorm:"type:jsonb"`
	OutputKey      string
	AttemptToken   *uuid.UUID `gorm:"type:uuid"`
	LeaseUntil     *time.Time
	Attempts       int
	FailureCode    string
	FailureMessage string
}

func (VideoEditorExport) TableName() string { return "video_editor_exports" }
