package storage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReachable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing.png" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte{0x89})
	}))
	defer srv.Close()
	if err := Reachable(context.Background(), srv.URL+"/ok.png"); err != nil {
		t.Fatal(err)
	}
	if err := Reachable(context.Background(), srv.URL+"/missing.png"); err == nil {
		t.Fatal("missing object reported reachable")
	}
}
