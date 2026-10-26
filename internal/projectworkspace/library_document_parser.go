package projectworkspace

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

const wordML = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
const contentTypesNS = "http://schemas.openxmlformats.org/package/2006/content-types"
const relationsNS = "http://schemas.openxmlformats.org/package/2006/relationships"

var documentUnsupported = errors.New("unsupported document structure")
var documentTooLarge = errors.New("document budget exceeded")
var documentInvalid = errors.New("invalid document package")

func validDOCXPath(name string) bool {
	return name != "" && len(name) <= 256 && utf8.ValidString(name) && !strings.ContainsAny(name, "\\\x00:") && !strings.HasPrefix(name, "/") && path.Clean(name) == name && name != "." && name != ".." && !strings.HasPrefix(name, "../")
}
func zipExtraSafe(extra []byte) bool {
	for len(extra) > 0 {
		if len(extra) < 4 {
			return false
		}
		id := binary.LittleEndian.Uint16(extra)
		n := int(binary.LittleEndian.Uint16(extra[2:]))
		extra = extra[4:]
		if id == 1 || n > len(extra) {
			return false
		}
		extra = extra[n:]
	}
	return true
}

// Validate a bounded central directory before archive/zip can allocate its File
// slice. ZIP64, multiple disks and ambiguous local/central identities fail closed.
func preflightDOCX(b []byte) error {
	if len(b) < 22 {
		return documentInvalid
	}
	end := -1
	for i := len(b) - 22; i >= 0 && i >= len(b)-65557; i-- {
		if binary.LittleEndian.Uint32(b[i:]) == 0x06054b50 && i+22+int(binary.LittleEndian.Uint16(b[i+20:])) == len(b) {
			end = i
			break
		}
	}
	if end < 0 {
		return documentInvalid
	}
	e := b[end:]
	count := int(binary.LittleEndian.Uint16(e[10:]))
	size := uint64(binary.LittleEndian.Uint32(e[12:]))
	off := uint64(binary.LittleEndian.Uint32(e[16:]))
	if binary.LittleEndian.Uint16(e[4:]) != 0 || binary.LittleEndian.Uint16(e[6:]) != 0 || binary.LittleEndian.Uint16(e[8:]) != uint16(count) || count == 65535 || size == 0xffffffff || off == 0xffffffff {
		return documentInvalid
	}
	if count > 256 {
		return documentTooLarge
	}
	if off+size != uint64(end) || off > uint64(len(b)) {
		return documentInvalid
	}
	pos := int(off)
	seen := map[string]bool{}
	var expanded uint64
	for i := 0; i < count; i++ {
		if pos+46 > end || binary.LittleEndian.Uint32(b[pos:]) != 0x02014b50 {
			return documentInvalid
		}
		c := b[pos:]
		flags := binary.LittleEndian.Uint16(c[8:])
		method := binary.LittleEndian.Uint16(c[10:])
		comp := binary.LittleEndian.Uint32(c[20:])
		raw := binary.LittleEndian.Uint32(c[24:])
		n := int(binary.LittleEndian.Uint16(c[28:]))
		x := int(binary.LittleEndian.Uint16(c[30:]))
		comment := int(binary.LittleEndian.Uint16(c[32:]))
		local := uint64(binary.LittleEndian.Uint32(c[42:]))
		next := pos + 46 + n + x + comment
		if next > end || flags&0x2041 != 0 || (method != 0 && method != 8) || comp == 0xffffffff || raw == 0xffffffff || local == 0xffffffff || binary.LittleEndian.Uint16(c[34:]) != 0 {
			return documentInvalid
		}
		name := string(c[46 : 46+n])
		canonicalName := name
		if strings.HasSuffix(name, "/") {
			canonicalName = strings.TrimSuffix(name, "/")
		}
		if !validDOCXPath(canonicalName) || seen[canonicalName] || !zipExtraSafe(c[46+n:46+n+x]) {
			return documentInvalid
		}
		seen[canonicalName] = true
		expanded += uint64(raw)
		if expanded > 32<<20 {
			return documentTooLarge
		}
		if local+30 > off {
			return documentInvalid
		}
		l := b[int(local):]
		ln := int(binary.LittleEndian.Uint16(l[26:]))
		lx := int(binary.LittleEndian.Uint16(l[28:]))
		data := local + 30 + uint64(ln+lx)
		if binary.LittleEndian.Uint32(l) != 0x04034b50 || data+uint64(comp) > off || binary.LittleEndian.Uint16(l[6:]) != flags || binary.LittleEndian.Uint16(l[8:]) != method || ln != n || string(l[30:30+ln]) != name || !zipExtraSafe(l[30+ln:30+ln+lx]) {
			return documentInvalid
		}
		if flags&8 == 0 && (binary.LittleEndian.Uint32(l[14:]) != binary.LittleEndian.Uint32(c[16:]) || binary.LittleEndian.Uint32(l[18:]) != comp || binary.LittleEndian.Uint32(l[22:]) != raw) {
			return documentInvalid
		}
		pos = next
	}
	if pos != end {
		return documentInvalid
	}
	return nil
}

func documentXML(body []byte, visit func(xml.Token, int) error) error {
	if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
		return documentInvalid
	}
	d := xml.NewDecoder(bytes.NewReader(body))
	depth := 0
	tokens := 0
	roots := 0
	sawDeclaration := false
	for {
		t, err := d.Token()
		if err == io.EOF {
			if depth != 0 || roots != 1 {
				return documentInvalid
			}
			return nil
		}
		if err != nil {
			return documentInvalid
		}
		tokens++
		if tokens > 262144 {
			return documentTooLarge
		}
		switch v := t.(type) {
		case xml.Directive:
			return documentInvalid
		case xml.ProcInst:
			if v.Target != "xml" || roots != 0 || sawDeclaration {
				return documentInvalid
			}
			sawDeclaration = true
		case xml.StartElement:
			seenAttrs := make(map[xml.Name]bool, len(v.Attr))
			for _, attr := range v.Attr {
				if seenAttrs[attr.Name] {
					return documentInvalid
				}
				seenAttrs[attr.Name] = true
			}
			if depth == 0 {
				roots++
			}
			depth++
			if depth > 64 {
				return documentTooLarge
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(v)) != "" {
				return documentInvalid
			}
		}
		if err = visit(t, depth); err != nil {
			return err
		}
		if _, ok := t.(xml.EndElement); ok {
			depth--
		}
	}
}
func xmlAttr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name && a.Name.Space == "" {
			return a.Value
		}
	}
	return ""
}

func parseDOCX(body []byte) (parts []LibraryDocumentPart, units []LibraryDocumentUnit, status string) {
	status = "invalid_document"
	err := preflightDOCX(body)
	if err != nil {
		if errors.Is(err, documentTooLarge) {
			status = "too_large"
		}
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	actual := 0
	read := func(name string) ([]byte, error) {
		f := files[name]
		if f == nil {
			return nil, documentInvalid
		}
		remaining := (8 << 20) - actual
		limit := 4 << 20
		if remaining < limit {
			limit = remaining
		}
		if f.UncompressedSize64 > uint64(limit) {
			return nil, documentTooLarge
		}
		r, e := f.Open()
		if e != nil {
			return nil, documentInvalid
		}
		b, e := io.ReadAll(io.LimitReader(r, int64(limit)+1))
		ce := r.Close()
		if e != nil || ce != nil {
			return nil, documentInvalid
		}
		actual += len(b)
		if len(b) > limit || actual > 8<<20 {
			return nil, documentTooLarge
		}
		return b, nil
	}
	types := map[string]string{}
	main := ""
	kinds := map[string]string{
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml": "document",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml":        "hdr",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml":        "ftr",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.footnotes+xml":     "footnotes",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.endnotes+xml":      "endnotes",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.comments+xml":      "comments",
	}
	run := func() error {
		b, e := read("[Content_Types].xml")
		if e != nil {
			return e
		}
		e = documentXML(b, func(t xml.Token, depth int) error {
			v, ok := t.(xml.StartElement)
			if !ok {
				return nil
			}
			if v.Name.Space != contentTypesNS {
				return documentInvalid
			}
			if depth == 1 && v.Name.Local != "Types" {
				return documentInvalid
			}
			if depth == 2 && v.Name.Local == "Override" {
				name := xmlAttr(v, "PartName")
				typ := xmlAttr(v, "ContentType")
				if !strings.HasPrefix(name, "/") || !validDOCXPath(strings.TrimPrefix(name, "/")) {
					return documentInvalid
				}
				name = name[1:]
				if _, ok := types[name]; ok {
					return documentInvalid
				}
				types[name] = typ
				if kinds[typ] == "document" {
					if main != "" {
						return documentInvalid
					}
					main = name
				}
			}
			return nil
		})
		if e != nil {
			return e
		}
		if main == "" {
			return documentUnsupported
		}
		if !strings.HasPrefix(main, "word/") {
			return documentInvalid
		}
		b, e = read("_rels/.rels")
		if e != nil {
			return e
		}
		found := false
		ids := map[string]bool{}
		e = documentXML(b, func(t xml.Token, depth int) error {
			v, ok := t.(xml.StartElement)
			if !ok {
				return nil
			}
			if v.Name.Space != relationsNS {
				return documentInvalid
			}
			if depth == 1 && v.Name.Local != "Relationships" {
				return documentInvalid
			}
			if depth == 2 && v.Name.Local == "Relationship" {
				id := xmlAttr(v, "Id")
				if id == "" || ids[id] {
					return documentInvalid
				}
				ids[id] = true
				typ := xmlAttr(v, "Type")
				if strings.HasSuffix(typ, "/officeDocument") {
					if typ != "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" {
						return documentUnsupported
					}
					mode := xmlAttr(v, "TargetMode")
					target := xmlAttr(v, "Target")
					if found || (mode != "" && mode != "Internal") || (target != main && target != "/"+main) {
						return documentInvalid
					}
					found = true
				}
			}
			return nil
		})
		if e != nil {
			return e
		}
		if !found {
			return documentInvalid
		}
		names := []string{}
		for name, typ := range types {
			if kinds[typ] != "" {
				if !strings.HasPrefix(name, "word/") {
					return documentInvalid
				}
				names = append(names, name)
			}
		}
		if len(names) > 64 {
			return documentTooLarge
		}
		sort.Strings(names)
		parts = []LibraryDocumentPart{}
		units = []LibraryDocumentUnit{}
		total := 0
		for _, name := range names {
			b, e = read(name)
			if e != nil {
				return e
			}
			part := LibraryDocumentPart{PartName: name, SourceSHA: libraryTextDigest(b)}
			inP := false
			inText := false
			isMain := name == main
			bodyCount := 0
			inBody := false
			var text strings.Builder
			e = documentXML(b, func(t xml.Token, depth int) error {
				switch v := t.(type) {
				case xml.StartElement:
					if inText {
						return documentInvalid
					}
					if depth == 1 {
						if v.Name.Space != wordML {
							return documentUnsupported
						}
						if v.Name.Local != kinds[types[name]] {
							return documentInvalid
						}
					}
					if v.Name.Space == "http://schemas.openxmlformats.org/markup-compatibility/2006" && v.Name.Local == "AlternateContent" {
						return documentUnsupported
					}
					if v.Name.Space == wordML {
						if strings.HasPrefix(v.Name.Local, "customXmlInsRange") || strings.HasPrefix(v.Name.Local, "customXmlDelRange") || strings.HasPrefix(v.Name.Local, "customXmlMove") {
							return documentUnsupported
						}
						switch v.Name.Local {
						case "body":
							if !isMain || depth != 2 || bodyCount != 0 || inBody {
								return documentInvalid
							}
							bodyCount++
							inBody = true
						case "ins", "del", "moveFrom", "moveTo", "moveFromRangeStart", "moveFromRangeEnd", "moveToRangeStart", "moveToRangeEnd", "altChunk", "sym", "subDoc", "pPrChange", "rPrChange", "sectPrChange", "tblPrChange", "trPrChange", "tcPrChange", "numberingChange", "cellIns", "cellDel", "cellMerge", "delText", "delInstrText":
							return documentUnsupported
						case "p":
							if isMain && !inBody {
								return documentInvalid
							}
							if inP {
								return documentUnsupported
							}
							inP = true
							part.ParagraphCount++
							text.Reset()
						case "t":
							if !inP || inText {
								return documentInvalid
							}
							inText = true
						case "tab":
							if inP {
								text.WriteByte('\t')
							}
						case "br", "cr":
							if inP {
								text.WriteByte('\n')
							}
						}
					}
				case xml.CharData:
					if inText {
						text.Write([]byte(v))
					}
				case xml.EndElement:
					if v.Name.Space == wordML {
						if v.Name.Local == "body" {
							inBody = false
						}
						if v.Name.Local == "t" {
							inText = false
						}
						if v.Name.Local == "p" {
							if !inP {
								return documentInvalid
							}
							inP = false
							s := text.String()
							total += len(s)
							if len(s) > 16384 || len(units) >= 8192 || total > 1<<20 {
								return documentTooLarge
							}
							units = append(units, LibraryDocumentUnit{Ordinal: len(units) + 1, PartName: name, ParagraphNumber: part.ParagraphCount, Text: s, TextHash: libraryTextDigest([]byte(s))})
						}
					}
				}
				if text.Len() > 16384 {
					return documentTooLarge
				}
				return nil
			})
			if e != nil {
				return e
			}
			if isMain && bodyCount != 1 {
				return documentInvalid
			}
			parts = append(parts, part)
		}
		return nil
	}
	err = run()
	if err != nil {
		parts = nil
		units = nil
		if errors.Is(err, documentUnsupported) {
			status = "unsupported"
		} else if errors.Is(err, documentTooLarge) {
			status = "too_large"
		}
		return
	}
	status = "ready"
	return
}
