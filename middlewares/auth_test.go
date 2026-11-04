package middlewares

import (
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"net/http/httptest"
	"testing"
	"time"
)

func TestJWTAuthRequiresExactMethodExpiryAndUUID(t *testing.T) {
	secret := "test-fixture-secret-at-least-32-bytes"
	for _, tc := range []struct {
		name   string
		method jwt.SigningMethod
		sub    string
		expiry *time.Time
		want   int
	}{{"valid", jwt.SigningMethodHS256, uuid.NewString(), ptrTime(time.Now().Add(time.Hour)), 200}, {"no expiry", jwt.SigningMethodHS256, uuid.NewString(), nil, 401}, {"expired", jwt.SigningMethodHS256, uuid.NewString(), ptrTime(time.Now().Add(-time.Minute)), 401}, {"bad subject", jwt.SigningMethodHS256, "not-a-uuid", ptrTime(time.Now().Add(time.Hour)), 401}, {"HS384 rejected", jwt.SigningMethodHS384, uuid.NewString(), ptrTime(time.Now().Add(time.Hour)), 401}} {
		t.Run(tc.name, func(t *testing.T) {
			claims := jwt.MapClaims{"sub": tc.sub}
			if tc.expiry != nil {
				claims["exp"] = tc.expiry.Unix()
			}
			token, e := jwt.NewWithClaims(tc.method, claims).SignedString([]byte(secret))
			if e != nil {
				t.Fatal(e)
			}
			app := fiber.New()
			app.Get("/", JWTAuth(secret), func(c *fiber.Ctx) error { return c.SendStatus(200) })
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			res, e := app.Test(r)
			if e != nil || res.StatusCode != tc.want {
				t.Fatalf("status %v err %v", res, e)
			}
			res.Body.Close()
		})
	}
}
func ptrTime(v time.Time) *time.Time { return &v }
