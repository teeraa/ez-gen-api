package utils

import (
	"os"
	"time"
	_ "time/tzdata" // embed the zone database so APP_TIMEZONE works in minimal images

	"github.com/gofiber/fiber/v2"
)

// TimestampLayout is ISO 8601 with microseconds and the zone offset,
// e.g. 2026-11-17T15:14:57.510357+07:00.
const TimestampLayout = "2006-01-02T15:04:05.000000Z07:00"

// Response is the standard envelope for every JSON response, success or error.
//
//	{
//	  "code": 200,
//	  "timestamp": "2026-11-17T15:14:57.510357+07:00",
//	  "transactionCode": "77c1f0b2-5474-42cd-a4a7-217129d87bb4",
//	  "success": true,
//	  "titleMessage": "",
//	  "message": "Success",
//	  "payload": {}
//	}
//
// code mirrors the HTTP status. transactionCode is the per-request ID (also sent
// as the X-Request-ID header). On errors payload is ErrorPayload.
type Response struct {
	Code            int    `json:"code"`
	Timestamp       string `json:"timestamp"`
	TransactionCode string `json:"transactionCode"`
	Success         bool   `json:"success"`
	TitleMessage    string `json:"titleMessage"`
	Message         string `json:"message"`
	Payload         any    `json:"payload"`
}

// ErrorPayload carries the machine-readable error code (e.g. VALIDATION_ERROR)
// and, for validation failures only, the per-field details.
type ErrorPayload struct {
	ErrorCode string `json:"errorCode"`
	Details   any    `json:"details,omitempty"`
}

type ValidationDetail struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

const successMessage = "Success"

var location = loadLocation()

// loadLocation reads APP_TIMEZONE (default Asia/Bangkok) for response timestamps.
func loadLocation() *time.Location {
	name := os.Getenv("APP_TIMEZONE")
	if name == "" {
		name = "Asia/Bangkok"
	}
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return time.FixedZone("ICT", 7*60*60)
}

// Now returns the current time formatted for the envelope timestamp.
func Now() string { return time.Now().In(location).Format(TimestampLayout) }

// Envelope builds a response body without writing it. Use it where the
// response is not sent through Success/Error (e.g. fiber's ErrorHandler).
func Envelope(c *fiber.Ctx, status int, success bool, message string, payload any) Response {
	rid, _ := c.Locals("requestID").(string)
	if payload == nil {
		payload = fiber.Map{}
	}
	return Response{
		Code:            status,
		Timestamp:       Now(),
		TransactionCode: rid,
		Success:         success,
		TitleMessage:    "",
		Message:         message,
		Payload:         payload,
	}
}

// Success writes a success envelope with data as payload.
func Success(c *fiber.Ctx, status int, data any) error {
	return c.Status(status).JSON(Envelope(c, status, true, successMessage, data))
}

// Error writes an error envelope. code is the machine-readable error code;
// details is only set for VALIDATION_ERROR.
func Error(c *fiber.Ctx, status int, code, message string, details any) error {
	return c.Status(status).JSON(Envelope(c, status, false, message, ErrorPayload{ErrorCode: code, Details: details}))
}
