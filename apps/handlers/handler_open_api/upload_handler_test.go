package handler_open_api

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/gofiber/fiber/v2"
)

type uploadCapture struct {
	calls int
	data  []byte
}

func (u *uploadCapture) CreateUpload(_ context.Context, _ string, in facade_open_api.CreateUploadInput) (*dto_open_api.UploadDTO, error) {
	u.calls++
	u.data = append([]byte(nil), in.Data...)
	return &dto_open_api.UploadDTO{UploadID: "fixture-upload"}, nil
}
func TestUploadHandlerAcceptsEightMiBMultipartAndReadsEntireFile(t *testing.T) {
	data := bytes.Repeat([]byte{0x47}, 8<<20)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "reference.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	capture := &uploadCapture{}
	app := fiber.New(fiber.Config{BodyLimit: maxUploadBodyBytes})
	app.Use(func(c *fiber.Ctx) error { c.Locals("userID", "fixture-user"); return c.Next() })
	NewUploadHandler(capture).RegisterRoutes(app)
	request := httptest.NewRequest("POST", "/uploads", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 201 || capture.calls != 1 || !bytes.Equal(capture.data, data) {
		t.Fatalf("status %d calls %d bytes %d", response.StatusCode, capture.calls, len(capture.data))
	}
}
