package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnsureBucketCreatesPrivateRestrictedBucket(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/storage/v1/bucket" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("unexpected request")
		}
		if r.Method == http.MethodGet {
			w.Write([]byte(`[]`))
			return
		}
		posts++
		var b bucket
		if json.NewDecoder(r.Body).Decode(&b) != nil || b.ID != "characters" || b.Public || b.FileSizeLimit == nil || *b.FileSizeLimit != 24<<20 || len(b.AllowedMimeTypes) != 2 {
			t.Error("bucket must be private and restricted")
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	if e := ensureBucket(context.Background(), server.Client(), server.URL, "fixture-key", "characters"); e != nil {
		t.Fatal(e)
	}
	if posts != 1 {
		t.Fatalf("writes=%d", posts)
	}
}
func TestEnsureBucketNeverMutatesExistingBucket(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		wantErr    bool
	}{
		{"private", `[{"id":"characters","public":false}]`, false},
		{"public", `[{"id":"characters","public":true}]`, true},
		{"small", `[{"id":"characters","public":false,"file_size_limit":1024}]`, true},
		{"wrong mime", `[{"id":"characters","public":false,"allowed_mime_types":["video/mp4"]}]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("mutated existing bucket")
				}
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			e := ensureBucket(context.Background(), server.Client(), server.URL, "fixture-key", "characters")
			if (e != nil) != tc.wantErr {
				t.Fatalf("error=%v", e)
			}
		})
	}
}
func TestEnsureBucketSanitizesFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte("fixture-key secret upstream details"))
	}))
	defer server.Close()
	e := ensureBucket(context.Background(), server.Client(), server.URL, "fixture-key", "characters")
	if e == nil || strings.Contains(e.Error(), "fixture-key") || strings.Contains(e.Error(), "upstream") {
		t.Fatalf("unsafe error %v", e)
	}
}
