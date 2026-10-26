package projectworkspace

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func documentFixtureZIP(t *testing.T, changes map[string]string) []byte {
	t.Helper()
	files := map[string]string{
		"[Content_Types].xml": `<Types xmlns="` + contentTypesNS + `"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/><Override PartName="/word/footnotes.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footnotes+xml"/></Types>`,
		"_rels/.rels":         `<Relationships xmlns="` + relationsNS + `"><Relationship Id="r1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<w:document xmlns:w="` + wordML + `"><w:body><w:p><w:r><w:t>Castle café 城</w:t><w:tab/><w:t>raw &amp; &lt;tag&gt;</w:t><w:br/><w:t>end</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>table</w:t></w:r></w:p></w:tc></w:tr></w:tbl><w:p/></w:body></w:document>`,
		"word/header1.xml":    `<w:hdr xmlns:w="` + wordML + `"><w:p><w:r><w:t>header</w:t></w:r></w:p></w:hdr>`,
		"word/footnotes.xml":  `<w:footnotes xmlns:w="` + wordML + `"><w:footnote w:id="1"><w:p><w:r><w:t>note</w:t></w:r></w:p></w:footnote></w:footnotes>`,
	}
	for k, v := range changes {
		if v == "DELETE" {
			delete(files, k)
		} else {
			files[k] = v
		}
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for k, v := range files {
		w, e := z.Create(k)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(v)); e != nil {
			t.Fatal(e)
		}
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestLibraryDocumentParserStoredParagraphs(t *testing.T) {
	b := documentFixtureZIP(t, nil)
	parts, units, status := parseDOCX(b)
	if status != "ready" || len(parts) != 3 || len(units) != 5 {
		t.Fatalf("%s %+v %+v", status, parts, units)
	}
	want := []string{"Castle café 城\traw & <tag>\nend", "table", "", "note", "header"}
	for i, u := range units {
		if u.Ordinal != i+1 || u.Text != want[i] || u.TextHash != libraryTextDigest([]byte(want[i])) {
			t.Fatalf("unit %+v", u)
		}
	}
	if parts[0].ParagraphCount != 3 || units[3].PartName != "word/footnotes.xml" || units[3].ParagraphNumber != 1 {
		t.Fatal(parts, units)
	}
	v := LibraryVersion{AssetID: "a", Version: 1, ContentHash: libraryTextDigest(b), SizeBytes: int64(len(b)), MIMEType: LibraryDocumentMIME}
	idx, _, err := parseLibraryDocument(v, b)
	if err != nil || idx.RawSourceSHA == idx.DerivedSHA || idx.Coverage != LibraryDocumentCoverage || VerifyLibraryDocumentContent(idx, b) != nil {
		t.Fatal(idx, err)
	}
	idx.Parts[0].SourceSHA = libraryTextDigest([]byte("forged"))
	if VerifyLibraryDocumentContent(idx, b) == nil {
		t.Fatal("forged part receipt accepted")
	}
	idx, forged, err := parseLibraryDocument(v, b)
	if err != nil {
		t.Fatal(err)
	}
	forged[0].Text = "self-consistent forged paragraph"
	forged[0].TextHash = libraryTextDigest([]byte(forged[0].Text))
	idx.DerivedSHA, idx.DerivedUTF8Bytes = documentCanonical(forged)
	idx.manifestSHA = documentManifest(idx)
	if VerifyLibraryDocumentContent(idx, b) == nil {
		t.Fatal("self-consistent forged derived receipt accepted for original source")
	}
}

func TestLibraryDocumentParserCanonicalDirectories(t *testing.T) {
	b := documentFixtureZIP(t, map[string]string{"word/": "", "_rels/": ""})
	_, u, status := parseDOCX(b)
	if status != "ready" || len(u) != 5 {
		t.Fatal(status, len(u))
	}
	// A same-path directory/file alias cannot create ambiguous ZIP identities.
	if preflightDOCX(documentFixtureZIP(t, map[string]string{"word": "", "word/": ""})) == nil {
		t.Fatal("directory/file alias accepted")
	}
}
func TestLibraryDocumentParserRejectsAmbiguityAndBounds(t *testing.T) {
	story := func(s string) string {
		return `<w:document xmlns:w="` + wordML + `"><w:body>` + s + `</w:body></w:document>`
	}
	cases := []struct{ name, body, status string }{
		{"tracked", story(`<w:ins><w:p><w:r><w:t>hidden</w:t></w:r></w:p></w:ins>`), "unsupported"},
		{"move", story(`<w:p><w:moveFrom/></w:p>`), "unsupported"},
		{"altChunk", story(`<w:altChunk/>`), "unsupported"},
		{"alternate", story(`<mc:AlternateContent xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"/>`), "unsupported"},
		{"symbol", story(`<w:p><w:r><w:sym/></w:r></w:p>`), "unsupported"},
		{"nested", story(`<w:p><w:p/></w:p>`), "unsupported"},
		{"strict", `<w:document xmlns:w="http://purl.oclc.org/ooxml/wordprocessingml/main"/>`, "unsupported"},
		{"doctype", `<!DOCTYPE x [<!ENTITY x "payload">]>` + story(`<w:p><w:r><w:t>&x;</w:t></w:r></w:p>`), "invalid_document"},
		{"entity", story(`<w:p><w:r><w:t>&unknown;</w:t></w:r></w:p>`), "invalid_document"},
		{"malformed", story(`<w:p>`), "invalid_document"},
		{"duplicate-attribute", story(`<w:p w:rsidR="one" w:rsidR="two"/>`), "invalid_document"},
		{"unit", story(`<w:p><w:r><w:t>` + strings.Repeat("x", 16385) + `</w:t></w:r></w:p>`), "too_large"},
		{"count", story(strings.Repeat(`<w:p/>`, 8193)), "too_large"},
		{"output", story(strings.Repeat(`<w:p><w:r><w:t>`+strings.Repeat("x", 16384)+`</w:t></w:r></w:p>`, 65)), "too_large"},
		{"depth", story(strings.Repeat(`<w:r>`, 65) + strings.Repeat(`</w:r>`, 65)), "too_large"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, u, s := parseDOCX(documentFixtureZIP(t, map[string]string{"word/document.xml": tc.body}))
			if s != tc.status || p != nil || u != nil {
				t.Fatalf("got %s %d %d", s, len(p), len(u))
			}
		})
	}
	for _, name := range []string{"../outside.xml", "word\\bad.xml", "/absolute.xml", "word/../bad.xml"} {
		t.Run(name, func(t *testing.T) {
			_, _, s := parseDOCX(documentFixtureZIP(t, map[string]string{name: "test"}))
			if s != "invalid_document" {
				t.Fatal(s)
			}
		})
	}
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml"} {
		t.Run("missing"+name, func(t *testing.T) {
			_, _, s := parseDOCX(documentFixtureZIP(t, map[string]string{name: "DELETE"}))
			if s != "invalid_document" {
				t.Fatal(s)
			}
		})
	}
}
func TestLibraryDocumentZIPPreflight(t *testing.T) {
	original := documentFixtureZIP(t, nil)
	for _, tc := range []struct {
		name   string
		mutate func([]byte)
	}{
		{"encrypted", func(b []byte) { p := bytes.Index(b, []byte{'P', 'K', 1, 2}); b[p+8] |= 1 }},
		{"zip64", func(b []byte) {
			p := bytes.LastIndex(b, []byte{'P', 'K', 5, 6})
			binary.LittleEndian.PutUint16(b[p+10:], 65535)
		}},
		{"entries", func(b []byte) {
			p := bytes.LastIndex(b, []byte{'P', 'K', 5, 6})
			binary.LittleEndian.PutUint16(b[p+8:], 257)
			binary.LittleEndian.PutUint16(b[p+10:], 257)
		}},
		{"local mismatch", func(b []byte) { b[6] ^= 0x10 }},
		{"declared bomb", func(b []byte) {
			p := bytes.Index(b, []byte{'P', 'K', 1, 2})
			binary.LittleEndian.PutUint32(b[p+24:], 33<<20)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := append([]byte(nil), original...)
			tc.mutate(b)
			if preflightDOCX(b) == nil {
				t.Fatal("accepted")
			}
		})
	}
	for _, b := range [][]byte{nil, []byte("PK"), original[:len(original)-1]} {
		if preflightDOCX(b) == nil {
			t.Fatal("truncated accepted")
		}
	}
}

func TestLibraryDocumentParserAggregateAndMetadataBudgets(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changes map[string]string
	}{
		{"xml-part", map[string]string{"word/header1.xml": `<w:hdr xmlns:w="` + wordML + `"><!--` + strings.Repeat("x", 4<<20) + `--></w:hdr>`}},
		{"xml-aggregate", map[string]string{"word/document.xml": `<w:document xmlns:w="` + wordML + `"><w:body><!--` + strings.Repeat("x", 3<<20) + `--></w:body></w:document>`, "word/header1.xml": `<w:hdr xmlns:w="` + wordML + `"><!--` + strings.Repeat("x", 3<<20) + `--></w:hdr>`, "word/footnotes.xml": `<w:footnotes xmlns:w="` + wordML + `"><!--` + strings.Repeat("x", 3<<20) + `--></w:footnotes>`}},
		{"tokens", map[string]string{"word/header1.xml": `<w:hdr xmlns:w="` + wordML + `">` + strings.Repeat(`<!---->`, 262145) + `</w:hdr>`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, s := parseDOCX(documentFixtureZIP(t, tc.changes))
			if s != "too_large" {
				t.Fatal(s)
			}
		})
	}
}

func TestLibraryDocumentParserMainBodyAndOfficeTarget(t *testing.T) {
	main := func(s string) string { return `<w:document xmlns:w="` + wordML + `">` + s + `</w:document>` }
	for _, tc := range []struct{ name, xml, status string }{
		{"minimal-body", main(`<w:body/>`), "ready"},
		{"minimal-paragraph", main(`<w:body><w:p><w:r><w:t>stored</w:t></w:r></w:p></w:body>`), "ready"},
		{"no-body", main(""), "invalid_document"},
		{"duplicate-body", main(`<w:body/><w:body/>`), "invalid_document"},
		{"nested-body", main(`<w:body><w:body/></w:body>`), "invalid_document"},
		{"deep-body", main(`<w:customXml><w:body/></w:customXml>`), "invalid_document"},
		{"paragraph-before-body", main(`<w:p/><w:body/>`), "invalid_document"},
		{"paragraph-after-body", main(`<w:body/><w:p/>`), "invalid_document"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, status := parseDOCX(documentFixtureZIP(t, map[string]string{"word/document.xml": tc.xml}))
			if status != tc.status {
				t.Fatal(status)
			}
		})
	}
	for _, tc := range []struct{ name, target, mode, status string }{
		{"relative", "word/document.xml", "", "ready"},
		{"absolute", "/word/document.xml", "", "ready"},
		{"explicit-internal", "/word/document.xml", "Internal", "ready"},
		{"unknown-mode", "word/document.xml", "Unknown", "invalid_document"},
		{"external-mode", "word/document.xml", "External", "invalid_document"},
		{"external-url", "https://example.invalid/document.xml", "External", "invalid_document"},
		{"noncanonical", "word/../word/document.xml", "Internal", "invalid_document"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rels := `<Relationships xmlns="` + relationsNS + `"><Relationship Id="r1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="` + tc.target + `" TargetMode="` + tc.mode + `"/></Relationships>`
			_, _, status := parseDOCX(documentFixtureZIP(t, map[string]string{"_rels/.rels": rels}))
			if status != tc.status {
				t.Fatal(status)
			}
		})
	}
}

func TestLibraryDocumentParserPartAndEntryBudgets(t *testing.T) {
	changes := map[string]string{}
	for i := 0; i < 252; i++ {
		changes["extras/"+strings.Repeat("x", i+1)] = ""
	}
	_, _, status := parseDOCX(documentFixtureZIP(t, changes))
	if status != "too_large" {
		t.Fatal("entry budget", status)
	}
	var ct strings.Builder
	ct.WriteString(`<Types xmlns="` + contentTypesNS + `"><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>`)
	for i := 0; i < 64; i++ {
		name := "word/header" + strings.Repeat("x", i+1) + ".xml"
		ct.WriteString(`<Override PartName="/` + name + `" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/>`)
		changes[name] = `<w:hdr xmlns:w="` + wordML + `"/>`
	}
	ct.WriteString(`</Types>`)
	changes = map[string]string{"[Content_Types].xml": ct.String()}
	_, _, status = parseDOCX(documentFixtureZIP(t, changes))
	if status != "too_large" {
		t.Fatal("part budget", status)
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for i := 0; i < 2; i++ {
		w, err := z.Create("same")
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("data"))
	}
	z.Close()
	if preflightDOCX(b.Bytes()) == nil {
		t.Fatal("duplicate ZIP name")
	}
}
