package middlewares

import (
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gofiber/fiber/v2"
)

type JWTClaims struct {
	Plan string `json:"plan"`
	jwt.RegisteredClaims
}

func JWTAuth(secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return respondUnauthorized(c)
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		token, err := jwt.ParseWithClaims(tokenStr, &JWTClaims{}, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fiber.ErrUnauthorized
			}
			return []byte(secret), nil
		})
		if err != nil || !token.Valid {
			return respondUnauthorized(c)
		}

		claims, ok := token.Claims.(*JWTClaims)
		if !ok {
			return respondUnauthorized(c)
		}

		userID := claims.Subject
		if userID == "" {
			return respondUnauthorized(c)
		}

		c.Locals("userID", userID)
		c.Locals("userPlan", claims.Plan)
		return c.Next()
	}
}

// DevAuth injects a fixed dev userID so handlers work without a real JWT.
// Use ONLY in development — never enable in production.
func DevAuth(devUserID string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals("userID", devUserID)
		c.Locals("userPlan", "dev")
		return c.Next()
	}
}

func respondUnauthorized(c *fiber.Ctx) error {
	rid, _ := c.Locals("requestID").(string)
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
		"code":      "UNAUTHORIZED",
		"message":   "Valid authentication required",
		"requestId": rid,
	})
}
