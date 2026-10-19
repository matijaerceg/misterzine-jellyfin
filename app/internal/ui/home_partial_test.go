package ui

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A server whose Continue Watching hub fails and whose second library
// errors still shows the rows that answered; only a server that answers
// nothing fails the screen.
func TestHomeKeepsRowsThatAnswered(t *testing.T) {
	fail := map[string]bool{"/UserItems/Resume": true, "/Items/Latest?2": true}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := r.URL.Path
		if route == "/Items/Latest" {
			route += "?" + r.URL.Query().Get("parentId")
		}
		if fail[route] {
			http.Error(w, "boom", 500)
			return
		}
		switch route {
		case "/UserViews":
			io.WriteString(w, `{"Items":[{"Id":"1","Name":"Films","CollectionType":"movies"},{"Id":"2","Name":"Shows","CollectionType":"tvshows"}]}`)
		case "/Items/Latest?1":
			io.WriteString(w, `[{"Id":"10","Type":"Movie","Name":"A"}]`)
		case "/Shows/NextUp":
			jfPage(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := testClient(server.URL, "tok", t.TempDir())
	hubs, secs, err := fetchHome(client, nil, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("one failed library sank the home screen: %v", err)
	}
	if len(secs) == 0 {
		t.Fatal("the libraries did not come back with the rows")
	}
	if len(hubs) != 1 || !strings.HasSuffix(hubs[0].Title, "Films") || len(hubs[0].Items) != 2 {
		t.Fatalf("expected the Films row alone, got %+v", hubs)
	}

	fail["/Items/Latest?1"] = true
	if _, _, err := fetchHome(client, nil, log.New(io.Discard, "", 0)); err == nil {
		t.Fatal("a server that answered nothing did not fail")
	}

	fail["/UserViews"] = true
	if _, _, err := fetchHome(client, nil, log.New(io.Discard, "", 0)); err == nil {
		t.Fatal("an unreachable library list did not fail")
	}
}

func TestShortErrShowsPathAndCause(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "https://jellyfin.example.test/UserItems/Resume?limit=30&mediaTypes=Video", Err: errors.New("context deadline exceeded")}
	if got := shortErr(err); got != "/UserItems/Resume: context deadline exceeded" {
		t.Fatalf("got %q", got)
	}
	if got := shortErr(errors.New(strings.Repeat("x", 80))); len(got) != 70 {
		t.Fatalf("long error not shortened: %q", got)
	}
}
