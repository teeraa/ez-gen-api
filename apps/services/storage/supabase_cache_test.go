package storage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSignedURLCacheUsesWallClock(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		fmt.Fprintf(w, `{"signedURL":"/object/sign/b/k?token=t%d"}`, n)
	}))
	defer srv.Close()
	c, _ := NewSupabaseClient(srv.URL, "key", "b", srv.Client())
	first, _, e := c.SignedURL(context.Background(), "k", time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	if again, _, _ := c.SignedURL(context.Background(), "k", time.Hour); again != first || n != 1 {
		t.Fatal("a fresh URL is reused")
	}
	// The laptop slept: the stored expiry is past on the wall clock. It must not be reused.
	c.cache["k"] = cachedURL{url: first, expiresAt: time.Now().Round(0).Add(-time.Minute)}
	if next, _, _ := c.SignedURL(context.Background(), "k", time.Hour); next == first || n != 2 {
		t.Fatalf("expired URL reused: %s", next)
	}
}
