package handler_open_api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/ez-gen/ez-gen-api/middlewares"
	"github.com/gofiber/fiber/v2"
)

func TestVideoEditorUploadGuardRunsBeforeMultipartParsing(t *testing.T) {
	t.Parallel()
	const user = "11111111-1111-4111-8111-111111111111"
	app := fiber.New(fiber.Config{
		DisablePreParseMultipartForm: true,
		BodyLimit:                    55 << 20,
		DisableStartupMessage:        true,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return utils.Error(c, 500, "INTERNAL_ERROR", "unexpected framework failure", nil)
		},
	})
	app.Use(middlewares.RequestID())
	app.Use(func(c *fiber.Ctx) error { c.Locals("userID", user); return c.Next() })
	handler := NewVideoEditorHandler(nil)
	// No facade is needed: admitted requests must be rejected before parsing while
	// the existing two upload slots are occupied.
	handler.uploads[user] = 2
	handler.RegisterRoutes(app.Group("/open/v1"))
	app.Post("/legacy-upload", func(c *fiber.Ctx) error {
		file, err := c.FormFile("file")
		if err != nil {
			return c.SendStatus(422)
		}
		reader, err := file.Open()
		if err != nil {
			return c.SendStatus(500)
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			return c.SendStatus(500)
		}
		return c.Send(data)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- app.Listener(listener) }()
	t.Cleanup(func() {
		_ = app.ShutdownWithTimeout(time.Second)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("server failed to stop")
		}
	})
	base := "http://" + listener.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second}
	var valid bytes.Buffer
	writer := multipart.NewWriter(&valid)
	part, err := writer.CreateFormFile("file", "example.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write([]byte("valid multipart body")); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, kind string
		data       []byte
	}{{"well-formed", writer.FormDataContentType(), valid.Bytes()}, {"malformed", "multipart/form-data; boundary=missing", []byte("not multipart at all")}} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, base+"/open/v1/video-editor/projects/11111111-1111-4111-8111-111111111111/assets", bytes.NewReader(tc.data))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", tc.kind)
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var body struct {
				Code      string `json:"code"`
				RequestID string `json:"requestId"`
			}
			if err = json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != 429 || body.Code != "UPLOAD_BUSY" || body.RequestID == "" {
				t.Fatalf("guard bypassed: status%d body%+v", response.StatusCode, body)
			}
		})
	}
	request, err := http.NewRequest(http.MethodPost, base+"/legacy-upload", bytes.NewReader(valid.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || strings.TrimSpace(string(data)) != "valid multipart body" {
		t.Fatalf("on-demand legacy FormFile failed: status%d body%q", response.StatusCode, data)
	}
}
