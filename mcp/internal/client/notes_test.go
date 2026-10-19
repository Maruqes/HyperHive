package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotesListContract(t *testing.T) {
	calls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/api/notes/" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Errorf("unexpected wire %s %s", r.Method, r.URL)
		}
		fmt.Fprint(w, `[{"id":9007199254740993,"titulo":"","nota":"legacy","extra":false}]`)
	}))
	defer backend.Close()
	c, err := New(Config{BaseURL: backend.URL + "/api", Token: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	notes, err := c.ListNotes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(notes[0]["id"]) != "9007199254740993" || notes[0]["extra"] != false || calls != 1 {
		t.Fatalf("response changed %#v", notes)
	}
}
