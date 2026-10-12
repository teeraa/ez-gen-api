package utils

import "github.com/gofiber/fiber/v2"

func Success(c *fiber.Ctx, status int, data any) error {
	rid, _ := c.Locals("requestID").(string)
	return c.Status(status).JSON(fiber.Map{
		"data":      data,
		"requestId": rid,
	})
}

func Error(c *fiber.Ctx, status int, code, message string, details any) error {
	rid, _ := c.Locals("requestID").(string)
	m := fiber.Map{
		"code":      code,
		"message":   message,
		"requestId": rid,
	}
	if details != nil {
		m["details"] = details
	}
	return c.Status(status).JSON(m)
}

type ValidationDetail struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}
