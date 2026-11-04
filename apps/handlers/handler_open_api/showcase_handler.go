package handler_open_api

import (
	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
)

// ShowcaseHandler serves curated videos for the public landing page.
type ShowcaseHandler struct {
	facade facade_open_api.ShowcaseFacade
}

func NewShowcaseHandler(facade facade_open_api.ShowcaseFacade) *ShowcaseHandler {
	return &ShowcaseHandler{facade: facade}
}

// RegisterPublicRoutes mounts unauthenticated routes on the /public/v1 router.
func (h *ShowcaseHandler) RegisterPublicRoutes(r fiber.Router) {
	r.Get("/showcase", h.List)
}

func (h *ShowcaseHandler) List(c *fiber.Ctx) error {
	items, err := h.facade.List(c.Context())
	if err != nil {
		return mapFacadeError(c, err)
	}
	c.Set(fiber.HeaderCacheControl, "public, max-age=300")
	return utils.Success(c, fiber.StatusOK, fiber.Map{"items": items})
}
