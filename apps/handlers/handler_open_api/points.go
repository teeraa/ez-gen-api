package handler_open_api

import (
	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/gofiber/fiber/v2"
)

// insufficientPoints answers 402 INSUFFICIENT_POINTS when e is a failed point charge.
func insufficientPoints(c *fiber.Ctx, e error) (error, bool) {
	d, ok := facade_open_api.IsInsufficientPoints(e)
	if !ok {
		return nil, false
	}
	return utils.Error(c, fiber.StatusPaymentRequired, "INSUFFICIENT_POINTS", "พอยต์ไม่เพียงพอ กรุณาเติมพอยต์ก่อนลองใหม่", struct {
		Required int   `json:"required"`
		Balance  int64 `json:"balance"`
	}{d.Required, d.Balance}), true
}
