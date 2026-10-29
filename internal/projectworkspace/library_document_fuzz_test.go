package projectworkspace

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// Exercise both the ZIP boundary and XML carried by a valid ZIP. Arbitrary ZIP
// mutations mostly stop at CRC/identity checks, so a separate XML target is
// necessary to explore the parser after those checks succeed.
func FuzzLibraryDocumentPackage(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("PK\x03\x04"))
	f.Add(documentFixtureZIP(f, nil))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > LibraryDocumentIndexLimit {
			t.Skip()
		}
		checkDocumentParseInvariants(t, raw)
	})
}

func FuzzLibraryDocumentWordML(f *testing.F) {
	f.Add("\ufeff" + `<w:document xmlns:w="` + wordML + `"><w:body><w:p/></w:body></w:document>`)
	f.Add(`<w:document xmlns:w="` + wordML + `"><w:body><w:p><w:r><w:t>café 城 &amp; text</w:t></w:r></w:p></w:body></w:document>`)
	f.Add(`<w:document xmlns:w="` + wordML + `"><w:body><w:p/></w:body></w:document>`)
	f.Add(`<!DOCTYPE x [<!ENTITY x "deny">]><x>&x;</x>`)
	f.Add(`<w:document xmlns:w="` + wordML + `"><w:body><w:p><w:p/></w:p></w:body></w:document>`)
	f.Fuzz(func(t *testing.T, xml string) {
		if len(xml) > 4<<20 {
			t.Skip()
		}
		checkDocumentParseInvariants(t, documentFixtureZIP(t, map[string]string{"word/document.xml": xml}))
	})
}

func checkDocumentParseInvariants(t *testing.T, raw []byte) {
	t.Helper()
	parts, units, status := parseDOCX(raw)
	switch status {
	case "invalid_document", "unsupported", "too_large":
		if len(parts) != 0 || len(units) != 0 {
			t.Fatal("non-ready parser leaked partial document output")
		}
		return
	case "ready":
	default:
		t.Fatalf("unknown parser outcome %q", status)
	}
	if len(parts) < 1 || len(parts) > 64 || len(units) > 8192 {
		t.Fatal("ready document exceeded part/unit limits")
	}
	counts := make(map[string]int)
	for i, p := range parts {
		canonicalSHA, validSHA := libraryTextCanonicalHash(p.SourceSHA)
		if !validDOCXPath(p.PartName) || !strings.HasPrefix(p.PartName, "word/") || p.ParagraphCount < 0 || !validSHA || canonicalSHA != p.SourceSHA || (i > 0 && parts[i-1].PartName >= p.PartName) {
			t.Fatal("invalid or noncanonical part receipt")
		}
		counts[p.PartName] = 0
	}
	total := 0
	for i, u := range units {
		count, exists := counts[u.PartName]
		if !exists || u.Ordinal != i+1 || u.ParagraphNumber != count+1 || len(u.Text) > 16384 || !utf8.ValidString(u.Text) || u.TextHash != libraryTextDigest([]byte(u.Text)) {
			t.Fatal("invalid paragraph identity, text or digest")
		}
		if i > 0 && units[i-1].PartName > u.PartName {
			t.Fatal("noncanonical paragraph order")
		}
		counts[u.PartName]++
		total += len(u.Text)
	}
	if total > 1<<20 {
		t.Fatal("ready document exceeded derived-text budget")
	}
	for _, p := range parts {
		if counts[p.PartName] != p.ParagraphCount {
			t.Fatal("part count differs from derived units")
		}
	}
	partsAgain, unitsAgain, statusAgain := parseDOCX(raw)
	if statusAgain != status || !reflect.DeepEqual(parts, partsAgain) || !reflect.DeepEqual(units, unitsAgain) {
		t.Fatal("same source bytes produced different document receipts")
	}
}
