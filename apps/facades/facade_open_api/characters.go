package facade_open_api

import (
	"context"
	"fmt"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/google/uuid"
)

// parseCharacterIDs enforces the per-scene limit and rejects duplicates, so the
// facade stays correct even for callers that skip request validation.
func parseCharacterIDs(raw []string) ([]uuid.UUID, error) {
	if len(raw) > models.MaxSceneCharacters {
		return nil, ErrValidation
	}
	ids := make([]uuid.UUID, 0, len(raw))
	seen := make(map[uuid.UUID]bool, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil || seen[id] {
			return nil, ErrValidation
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

// loadOwnedCharacters returns the profiles in the order given and fails with ErrNotFound
// when any of them is missing or belongs to another user, without saying which.
func loadOwnedCharacters(ctx context.Context, repo repository_open_api.CharacterProfileRepository, ids []uuid.UUID, userID string) ([]models.CharacterProfile, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	profiles, err := repo.FindByIDs(ctx, ids, userID)
	if err != nil {
		return nil, fmt.Errorf("load characters: %w", err)
	}
	if len(profiles) != len(ids) {
		return nil, ErrNotFound
	}
	return profiles, nil
}

// requestedCharacterIDs merges the list field with the legacy single field; the list
// wins when both are sent.
func requestedCharacterIDs(list []string, legacy *string) []string {
	if len(list) > 0 {
		return list
	}
	if legacy != nil && *legacy != "" {
		return []string{*legacy}
	}
	return nil
}

func sceneCharacterIDs(s *models.Scene) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(s.Characters))
	for _, c := range s.Characters {
		ids = append(ids, c.CharacterProfileID)
	}
	return ids
}
