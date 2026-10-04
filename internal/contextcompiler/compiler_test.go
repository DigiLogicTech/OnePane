package contextcompiler

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestHistoricalContextDropsBeforeCurrent(t *testing.T) {
	sections := []Section{
		{ID: "current", Kind: "task", Trust: "AUTHORITATIVE_DATA", Authoritative: true, Required: true, Priority: 100, Content: json.RawMessage(`{"state":"running"}`)},
		{ID: "recent", Kind: "evidence", Trust: "VERIFIED_DERIVED", Priority: 50, Content: json.RawMessage(`{"x":"12345678901234567890"}`)},
		{ID: "history", Kind: "history", Trust: "UNVERIFIED_DERIVED", Historical: true, Priority: 99, Content: json.RawMessage(`{"x":"12345678901234567890"}`)},
	}
	full, err := Compile(10000, sections)
	if err != nil {
		t.Fatal(err)
	}
	budget := full.Manifest.UsedBytes - 1
	got, err := Compile(budget, sections)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sections) != 2 {
		t.Fatalf("sections=%v manifest=%+v", got.Sections, got.Manifest)
	}
	for _, s := range got.Sections {
		if s.ID == "history" {
			t.Fatal("historical section survived while current optional context was available")
		}
	}
}
func TestRequiredContextNeverSilentlyTruncates(t *testing.T) {
	_, err := Compile(1, []Section{{ID: "required", Kind: "task", Trust: "AUTHORITATIVE_DATA", Required: true, Content: json.RawMessage(`{"x":1}`)}})
	if !errors.Is(err, ErrRequiredOverflow) {
		t.Fatalf("err=%v", err)
	}
}
func TestCompileIsDeterministic(t *testing.T) {
	a := []Section{{ID: "b", Kind: "x", Trust: "UNVERIFIED_DERIVED", Priority: 1, Content: json.RawMessage(`{"b":2}`)}, {ID: "a", Kind: "x", Trust: "UNVERIFIED_DERIVED", Priority: 1, Content: json.RawMessage(`{"a":1}`)}}
	r1, err := Compile(1000, a)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Compile(1000, a)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Manifest.ContextHash != r2.Manifest.ContextHash || string(r1.ManifestJSON) != string(r2.ManifestJSON) {
		t.Fatal("compiler output changed")
	}
}
