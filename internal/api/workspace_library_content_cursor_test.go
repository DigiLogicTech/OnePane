package api

import (
	"fmt"
	"testing"
	"time"
)

func TestLibraryDocumentContentCursorMemoryAndIdentityBounds(t *testing.T) {
	now := time.Unix(1000, 0)
	c := libraryContentSearchCursorRegistry{now: func() time.Time { return now }}
	binding := libraryContentCursorBinding{principal: "human", credential: "credential", tenant: "tenant", project: "project", workspace: "workspace", method: "bearer", principalType: "human"}
	position := libraryContentSearchPosition{}
	var first, last string
	for i := 0; i < 33; i++ {
		token, err := c.put(binding, position)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = token
		}
		last = token
		now = now.Add(time.Second)
	}
	if len(c.entries) != 32 {
		t.Fatal("per-credential navigation cache unbounded")
	}
	if _, ok := c.get(first, binding); ok {
		t.Fatal("oldest credential cursor not evicted")
	}
	if _, ok := c.get(last, binding); !ok {
		t.Fatal("current cursor lost")
	}
	for _, field := range []string{"tenant", "project", "workspace", "principal", "credential", "method", "principalType", "scopes", "query"} {
		changed := binding
		switch field {
		case "tenant":
			changed.tenant = "other"
		case "project":
			changed.project = "other"
		case "workspace":
			changed.workspace = "other"
		case "principal":
			changed.principal = "other"
		case "credential":
			changed.credential = "other"
		case "method":
			changed.method = "browser"
		case "principalType":
			changed.principalType = "agent"
		case "scopes":
			changed.scopes[0] = 1
		case "query":
			changed.query[0] = 1
		}
		if _, ok := c.get(last, changed); ok {
			t.Fatalf("cursor transferred across %s", field)
		}
	}
	for i := 0; i < 300; i++ {
		other := binding
		other.credential = fmt.Sprint(i)
		if _, err := c.put(other, position); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Millisecond)
	}
	if len(c.entries) > 256 {
		t.Fatal("global navigation cache unbounded")
	}
	now = now.Add(5 * time.Minute)
	if _, ok := c.get(last, binding); ok {
		t.Fatal("expired navigation retained")
	}
}
