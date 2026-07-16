package search

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"strings"
)

// OOXML documents are ZIP archives of XML parts — the stdlib covers
// both, so DOCX and XLSX extraction needs no dependencies at all.

const (
	typeDocx = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	typeXlsx = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
)

type docxExtractor struct{}

func (docxExtractor) Name() string { return "docx" }

func (docxExtractor) Claims(mediaType, name string) bool {
	return mediaType == typeDocx || strings.HasSuffix(strings.ToLower(name), ".docx")
}

// Extract walks word/document.xml collecting the character data of
// every <w:t> run and inserting a newline at each paragraph close —
// enough structure for search; layout is not the goal.
func (docxExtractor) Extract(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	found := false
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		found = true
		if err := collectXML(f, &b, map[string]bool{"t": true}, map[string]bool{"p": true}); err != nil {
			return "", err
		}
	}
	if !found {
		return "", errors.New("docx: no word/document.xml")
	}
	return clean(b.String()), nil
}

type xlsxExtractor struct{}

func (xlsxExtractor) Name() string { return "xlsx" }

func (xlsxExtractor) Claims(mediaType, name string) bool {
	return mediaType == typeXlsx || strings.HasSuffix(strings.ToLower(name), ".xlsx")
}

// Extract collects <t> character data from xl/sharedStrings.xml (where
// nearly all cell text lives) and from the worksheets themselves
// (inline strings). Numbers and formulas are not text and are skipped.
func (xlsxExtractor) Extract(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	found := false
	for _, f := range zr.File {
		if f.Name != "xl/sharedStrings.xml" &&
			!(strings.HasPrefix(f.Name, "xl/worksheets/") && path.Ext(f.Name) == ".xml") {
			continue
		}
		found = true
		if err := collectXML(f, &b, map[string]bool{"t": true}, map[string]bool{"si": true, "row": true}); err != nil {
			return "", err
		}
	}
	if !found {
		return "", errors.New("xlsx: no text-bearing parts")
	}
	return clean(b.String()), nil
}

// collectXML streams one ZIP part, appending character data that
// appears inside any element whose local name is in textIn (separated
// by spaces), plus a newline when an element in breakOn closes.
func collectXML(f *zip.File, b *strings.Builder, textIn, breakOn map[string]bool) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	dec := xml.NewDecoder(io.LimitReader(rc, MaxInput))
	depth := 0 // nesting depth inside textIn elements
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if textIn[t.Name.Local] {
				depth++
			}
		case xml.EndElement:
			if textIn[t.Name.Local] && depth > 0 {
				depth--
				b.WriteByte(' ')
			}
			if breakOn[t.Name.Local] {
				b.WriteByte('\n')
			}
		case xml.CharData:
			if depth > 0 {
				b.Write(t)
			}
		}
		if b.Len() > MaxText { // stop early once the cap is hit
			return nil
		}
	}
}
