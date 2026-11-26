package facade_open_api

// Film skills: the user picks up to filmSkillsMax "film" skills (Skills menu, output "film") for an
// episode. Each skill's directive is copied into the episode document when chosen, so the episode
// keeps working the same way if the skill is edited or deleted later; choosing it again refreshes it.
// Every plan job (whole episode, one shot, or position edits) passes the chosen directives to the LLM.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/google/uuid"
)

const filmSkillsMax = 3

// filmSkill is a chosen skill as stored in the episode document.
type filmSkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Description string `json:"description,omitempty"`
	Directive   string `json:"directive"`
}

// filmPlanSkill is a chosen skill as the planner sees it.
type filmPlanSkill struct {
	Name      string `json:"name"`
	Directive string `json:"directive"`
}

// filmSkillFinder loads a skill the user can see (built-in or own, not deleted).
type filmSkillFinder func(ctx context.Context, id, userID uuid.UUID) (*models.Skill, error)

// WithSkills enables choosing Skills-menu film skills for an episode.
func (f *FilmFacade) WithSkills(find filmSkillFinder) *FilmFacade {
	f.skills = find
	return f
}

func filmPlanSkills(d *filmDocument) []filmPlanSkill {
	out := make([]filmPlanSkill, 0, len(d.Skills))
	for _, s := range d.Skills {
		out = append(out, filmPlanSkill{Name: s.Name, Directive: s.Directive})
	}
	return out
}

func filmSkillsDTO(d *filmDocument) []dto.FilmSkill {
	out := make([]dto.FilmSkill, 0, len(d.Skills))
	for _, s := range d.Skills {
		out = append(out, dto.FilmSkill{ID: s.ID, Name: s.Name, Icon: s.Icon, Description: s.Description})
	}
	return out
}

// filmSkillsText is the planner prompt section of the episode's chosen skills ("" when none).
// Technical rules (one movement per shot, continuity, seconds, cast) stay in force; a skill's look,
// lens, light, composition and coverage choices replace the default composition guidance.
func filmSkillsText(skills []filmPlanSkill) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Director skills chosen by the user for this episode. Apply them to every new or changed shot (camera, action composition, light and audio). Where a skill conflicts with the composition or cliche guidance above, the skill wins; it never overrides the technical rules (one movement per shot, continuity inside a scene, shot seconds, cast and dialogue limits).\n")
	for _, s := range skills {
		fmt.Fprintf(&b, "- Skill \"%s\": %s\n", s.Name, s.Directive)
	}
	return b.String()
}

// SetSkills replaces the episode's chosen film skills (an empty list clears them).
func (f *FilmFacade) SetSkills(c context.Context, projectID, user string, in req.FilmSkillsRequest) (dto.FilmEpisode, error) {
	if !filmUUID(projectID) {
		return dto.FilmEpisode{}, ErrFilmNotFound
	}
	if !filmUUID(in.IdempotencyKey) {
		return dto.FilmEpisode{}, directorInvalid("idempotencyKey")
	}
	if f.skills == nil {
		return dto.FilmEpisode{}, &DirectorError{Status: 503, Code: "SKILLS_UNAVAILABLE", Message: "ยังไม่ได้ตั้งค่าระบบสกิล"}
	}
	if len(in.SkillIDs) > filmSkillsMax {
		return dto.FilmEpisode{}, &DirectorError{Status: 422, Code: "VALIDATION_ERROR", Message: fmt.Sprintf("เลือกสกิลได้สูงสุด %d สกิลต่อตอน", filmSkillsMax)}
	}
	uid, e := uuid.Parse(user)
	if e != nil {
		return dto.FilmEpisode{}, ErrFilmNotFound
	}
	chosen := make([]filmSkill, 0, len(in.SkillIDs))
	seen := map[uuid.UUID]bool{}
	for _, raw := range in.SkillIDs {
		id, e := uuid.Parse(raw)
		if e != nil {
			return dto.FilmEpisode{}, directorInvalid("skillIds")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		s, e := f.skills(c, id, uid)
		if errors.Is(e, repo.ErrNotFound) {
			return dto.FilmEpisode{}, &DirectorError{Status: 404, Code: "SKILL_NOT_FOUND", Message: "ไม่พบสกิลที่เลือก อาจถูกลบไปแล้ว"}
		} else if e != nil {
			return dto.FilmEpisode{}, e
		}
		if s.Output != SkillOutputFilm || strings.TrimSpace(s.Directive) == "" {
			return dto.FilmEpisode{}, &DirectorError{Status: 422, Code: "SKILL_NOT_FILM", Message: fmt.Sprintf("สกิล “%s” ไม่ใช่สกิลสำหรับภาพยนตร์", s.Name)}
		}
		chosen = append(chosen, filmSkill{ID: s.ID.String(), Name: s.Name, Icon: s.Icon, Description: s.Description, Directive: s.Directive})
	}
	hash := storyHash(struct {
		Action, Project string
		Input           req.FilmSkillsRequest
	}{"film-skills", projectID, in})
	_, e = f.store.ChangeEpisode(c, projectID, user, in.IdempotencyKey, hash, func(ep *models.FilmEpisode, _ *models.DirectorProject, _ *models.DirectorSeries, _ []models.FilmElement) ([]models.FilmJob, error) {
		d, e := filmDecode(ep)
		if e != nil {
			return nil, e
		}
		d.Skills = chosen
		return nil, filmEncode(ep, &d)
	})
	if e != nil {
		return dto.FilmEpisode{}, e
	}
	return f.GetEpisode(c, projectID, user)
}
