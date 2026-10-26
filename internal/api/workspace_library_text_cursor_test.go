package api

import (
	"fmt"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func TestWorkspaceLibraryTextCursorExpiryBindingAndMemoryBounds(t *testing.T) {
	now := time.Unix(1000, 0)
	r := libraryTextSearchCursorRegistry{now: func() time.Time { return now }}
	b := libraryTextCursorBinding{tenant: "tenant", project: "project", workspace: "workspace", principal: "human", credential: "one"}
	p := projectworkspace.LibraryTextSearchPosition{AssetID: "PRIVATE_ASSET", Version: 2, AfterLine: 8}
	first, err := r.put(b, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 43 {
		t.Fatal("cursor does not use 32 random bytes")
	}
	other := b
	other.workspace = "another"
	if _, ok := r.get(first, other); ok {
		t.Fatal("navigation crossed scope")
	}
	if actual, ok := r.get(first, b); !ok || actual != p {
		t.Fatal("wrong-scope attempt consumed original cursor")
	}
	now = now.Add(5 * time.Minute)
	if _, ok := r.get(first, b); ok {
		t.Fatal("expired cursor remained usable")
	}
	var latest string
	for i := 0; i < 40; i++ {
		now = now.Add(time.Second)
		latest, err = r.put(b, p)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(r.entries) != 32 {
		t.Fatalf("credential cursor budget exceeded: %d", len(r.entries))
	}
	if _, ok := r.get(latest, b); !ok {
		t.Fatal("fresh cursor evicted")
	}
	for i := 0; i < 300; i++ {
		now = now.Add(time.Millisecond)
		other := b
		other.credential = fmt.Sprint(i)
		if _, err = r.put(other, p); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.entries) != 256 {
		t.Fatalf("global cursor budget exceeded: %d", len(r.entries))
	}
	if _, ok := r.get(latest, b); ok {
		t.Fatal("least-recent cursor was not evicted")
	}
}

func TestWorkspaceLibraryTextCursorConcurrentReadOnlyNavigation(t *testing.T) {
	r := libraryTextSearchCursorRegistry{}
	b := libraryTextCursorBinding{credential: "original", principal: "human"}
	p := projectworkspace.LibraryTextSearchPosition{AssetID: "asset", Version: 1, AfterLine: 8}
	token, err := r.put(b, p)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan bool, 32)
	for i := 0; i < 32; i++ {
		go func() { got, ok := r.get(token, b); done <- ok && got == p }()
	}
	for i := 0; i < 32; i++ {
		if !<-done {
			t.Fatal("concurrent read changed navigation")
		}
	}
}
