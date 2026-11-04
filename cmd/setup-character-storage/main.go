// Command setup-character-storage creates the private character bucket without
// changing any existing bucket or moving objects. It never invokes an AI model.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ez-gen/ez-gen-api/pkgs/configs"
	"github.com/joho/godotenv"
)

type bucket struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Public           bool     `json:"public"`
	FileSizeLimit    *int64   `json:"file_size_limit"`
	AllowedMimeTypes []string `json:"allowed_mime_types"`
}

func ensureBucket(ctx context.Context, client *http.Client, base, key, name string) error {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || key == "" || name == "" {
		return errors.New("storage configuration is incomplete or invalid")
	}
	endpoint := strings.TrimRight(base, "/") + "/storage/v1/bucket"
	request := func(method string, body []byte) (*http.Response, error) {
		req, e := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if e != nil {
			return nil, errors.New("invalid storage request")
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("apikey", key)
		req.Header.Set("Content-Type", "application/json")
		res, e := client.Do(req)
		if e != nil {
			return nil, errors.New("storage connection failed")
		}
		return res, nil
	}
	res, err := request(http.MethodGet, nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("bucket inspection failed (HTTP %d)", res.StatusCode)
	}
	var buckets []bucket
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&buckets) != nil {
		return errors.New("invalid bucket inspection response")
	}
	for _, b := range buckets {
		if b.ID != name && b.Name != name {
			continue
		}
		if b.Public {
			return errors.New("character bucket already exists but is public; refusing to change it")
		}
		if b.FileSizeLimit != nil && *b.FileSizeLimit < 24<<20 {
			return errors.New("existing character bucket size limit is below 24 MiB; review bucket configuration")
		}
		if len(b.AllowedMimeTypes) > 0 {
			allowed := map[string]bool{}
			for _, v := range b.AllowedMimeTypes {
				allowed[v] = true
			}
			if !allowed["image/*"] && (!allowed["image/png"] || !allowed["image/jpeg"]) {
				return errors.New("existing character bucket must allow PNG and JPEG")
			}
		}
		return nil
	}
	limit := int64(24 << 20)
	payload, _ := json.Marshal(bucket{ID: name, Name: name, Public: false, FileSizeLimit: &limit, AllowedMimeTypes: []string{"image/png", "image/jpeg"}})
	res, err = request(http.MethodPost, payload)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("private bucket creation failed (HTTP %d); rerun inspection if another operator created it", res.StatusCode)
	}
	return nil
}

func main() {
	_ = godotenv.Load()
	cfg := configs.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ensureBucket(ctx, &http.Client{Timeout: 25 * time.Second}, cfg.SupabaseURL, cfg.SupabaseServiceRoleKey, cfg.StorageImageBucket); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Private character bucket is ready; existing buckets and objects were not modified.")
}
