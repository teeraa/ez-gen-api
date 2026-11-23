package dto_open_api

import "time"

type FilmFailure struct {
	FailureCode    *string `json:"failureCode"`
	FailureMessage *string `json:"failureMessage"`
	CanRetry       bool    `json:"canRetry"`
}

type FilmElement struct {
	ID             string     `json:"id"`
	SeriesID       string     `json:"seriesId"`
	Kind           string     `json:"kind"`
	Name           string     `json:"name"`
	Spec           string     `json:"spec"`
	Revision       int        `json:"revision"`
	ImageStatus    string     `json:"imageStatus"`
	ImageURL       *string    `json:"imageUrl"`
	ImageExpiresAt *time.Time `json:"imageExpiresAt"`
	// SyntheticURL is the faceless costume reference of a realistic/cinematic character.
	SyntheticURL *string `json:"syntheticUrl"`
	// Prepared is true when the element has the reference a shot video needs (for realistic and
	// cinematic characters: the faceless costume reference).
	Prepared bool `json:"prepared"`
	// Voice is set only on characters that have one.
	Voice *CharacterVoiceDTO `json:"voice"`
	FilmFailure
	UpdatedAt time.Time `json:"updatedAt"`
}

type FilmElementList struct {
	Items []FilmElement `json:"items"`
}

type FilmLine struct {
	SpeakerID string `json:"speakerId"`
	Line      string `json:"line"`
	Delivery  string `json:"delivery"`
}

type FilmShotVideo struct {
	Status    string     `json:"status"`
	Progress  int        `json:"progress"`
	URL       *string    `json:"url"`
	ExpiresAt *time.Time `json:"expiresAt"`
	Stale     bool       `json:"stale"`
	ModelID   string     `json:"modelId"`
	Seconds   int        `json:"seconds"`
	// Mode is "reference" (built from element images), "continue" (from the previous clip's last
	// frame) or "finalize" (the full clip rendered from this shot's draft).
	Mode string `json:"mode"`
	// Draft marks a 480p preview; CanFinalize means it can still be turned into the full clip.
	Draft          bool `json:"draft"`
	CanFinalize    bool `json:"canFinalize"`
	PointsCharged  int  `json:"pointsCharged"`
	PointsRefunded int  `json:"pointsRefunded"`
	// Voice is the state of the dubbed dialogue: "current", "outdated" (a character's voice changed;
	// POST /redub updates it without a new video), "updating", "failed" or "legacy" (made before
	// dubbing was kept apart from the clip: only a new video changes its voices) or "overlap"
	// (dubbed over the provider's own speech; the clip is also stale and needs a new video).
	Voice        string  `json:"voice"`
	VoiceMessage *string `json:"voiceMessage"`
	FilmFailure
}

type FilmShot struct {
	ID           string         `json:"id"`
	Index        int            `json:"index"`
	Seconds      int            `json:"seconds"`
	Start        int            `json:"start"`
	Transition   string         `json:"transition"`
	CharacterIDs []string       `json:"characterIds"`
	LocationID   string         `json:"locationId"`
	PropIDs      []string       `json:"propIds"`
	Action       string         `json:"action"`
	Camera       string         `json:"camera"`
	Dialogue     []FilmLine     `json:"dialogue"`
	Audio        string         `json:"audio"`
	TrimIn       float64        `json:"trimIn"`
	TrimOut      float64        `json:"trimOut"`
	Video        *FilmShotVideo `json:"video"`
}

// FilmMusic is the episode's music track, mixed under the whole film from its start.
type FilmMusic struct {
	Name      string     `json:"name"`
	Seconds   float64    `json:"seconds"`
	Volume    float64    `json:"volume"`
	URL       *string    `json:"url"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

type FilmVideoModel struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	MinSeconds int    `json:"minSeconds"`
	MaxSeconds int    `json:"maxSeconds"`
	Audio      bool   `json:"audio"`
	Default    bool   `json:"default"`
	// Draft means the model offers 480p preview renders (priced by DraftPointsPer5s).
	Draft bool `json:"draft"`
}

type FilmEpisode struct {
	ProjectID     string           `json:"projectId"`
	SeriesID      string           `json:"seriesId"`
	EpisodeNumber *int             `json:"episodeNumber"`
	EpisodeTitle  *string          `json:"episodeTitle"`
	Revision      int              `json:"revision"`
	Style         string           `json:"style"`
	AspectRatio   string           `json:"aspectRatio"`
	Resolution    string           `json:"resolution"`
	Filmed        bool             `json:"filmed"`
	TotalSeconds  int              `json:"totalSeconds"`
	Shots         []FilmShot       `json:"shots"`
	Elements      []FilmElement    `json:"elements"`
	VideoModels   []FilmVideoModel `json:"videoModels"`
	ImagePoints   int              `json:"imagePoints"`
	// VideoPointsPer5s is the price of five seconds of video; a shot costs it pro rata, rounded up.
	VideoPointsPer5s int `json:"videoPointsPer5s"`
	// DraftPointsPer5s prices a 480p draft; 0 means drafts are off.
	DraftPointsPer5s int           `json:"draftPointsPer5s"`
	Balance          int64         `json:"balance"`
	PlanPoints       int           `json:"planPoints"`
	PlanAvailable    bool          `json:"planAvailable"`
	TargetSeconds    int           `json:"targetSeconds"`
	Story            *FilmStory    `json:"story"`
	Messages         []FilmMessage `json:"messages"`
	Plan             *FilmPlan     `json:"plan"`
	Export           *FilmExport   `json:"export"`
	ExportAvailable  bool          `json:"exportAvailable"`
	Music            *FilmMusic    `json:"music"`
	// RedubShots clips can take their characters' current voices for RedubPoints points.
	RedubShots     int  `json:"redubShots"`
	RedubPoints    int  `json:"redubPoints"`
	RedubAvailable bool `json:"redubAvailable"`
}

// FilmExport is the merged film of an episode; Stale means a clip changed after it was made.
type FilmExport struct {
	Status         string     `json:"status"`
	Progress       int        `json:"progress"`
	URL            *string    `json:"url"`
	ExpiresAt      *time.Time `json:"expiresAt"`
	Stale          bool       `json:"stale"`
	ClipCount      int        `json:"clipCount"`
	Skipped        int        `json:"skipped"`
	Subtitles      bool       `json:"subtitles"`
	FailureMessage *string    `json:"failureMessage"`
}

type FilmStory struct {
	Title    string `json:"title"`
	Logline  string `json:"logline"`
	Synopsis string `json:"synopsis"`
	Theme    string `json:"theme"`
}

type FilmMessage struct {
	Role   string    `json:"role"`
	Text   string    `json:"text"`
	ShotID string    `json:"shotId"`
	At     time.Time `json:"at"`
}

// FilmPlan is a running or failed chat planning request; nil when idle.
type FilmPlan struct {
	Status         string  `json:"status"`
	ShotID         string  `json:"shotId"`
	FailureMessage *string `json:"failureMessage"`
}
