package middlewares

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func RequestID() fiber.Handler {
	return func(c *fiber.Ctx) error {
		rid := uuid.New().String()
		c.Locals("requestID", rid)
		c.Set("X-Request-ID", rid)
		return c.Next()
	}
}
