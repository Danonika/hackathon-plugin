package tools

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

type xlsxSheet struct {
	Name  string
	Table *table
}

type xlsxSheetRef struct {
	Name string
	Path string
}

func readXLSXTable(path string, sheetName string) (*table, []string, error) {
	sheets, warnings, err := readXLSXSheets(path)
	if err != nil {
		return nil, warnings, err
	}
	if len(sheets) == 0 {
		return nil, warnings, fmt.Errorf("xlsx: no worksheets found")
	}
	if sheetName == "" {
		return sheets[0].Table, warnings, nil
	}
	for _, s := range sheets {
		if strings.EqualFold(s.Name, sheetName) {
			return s.Table, warnings, nil
		}
	}
	names := make([]string, 0, len(sheets))
	for _, s := range sheets {
		names = append(names, s.Name)
	}
	return nil, warnings, fmt.Errorf("xlsx: sheet %q not found, available: %s", sheetName, strings.Join(names, ", "))
}

func readXLSXSheets(path string) ([]xlsxSheet, []string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, err
	}
	defer zr.Close()

	warnings := []string{}
	var shared []string
	if f := zipFind(zr, "xl/sharedStrings.xml"); f != nil {
		shared, err = readSharedStrings(f)
		if err != nil {
			warnings = append(warnings, "xlsx: failed to read sharedStrings.xml: "+err.Error())
		}
	}

	refs := workbookSheets(zr)
	if len(refs) == 0 {
		files := sheetFiles(zr)
		for i, f := range files {
			refs = append(refs, xlsxSheetRef{Name: fmt.Sprintf("Sheet%d", i+1), Path: f.Name})
		}
	}
	if len(refs) == 0 {
		return nil, warnings, fmt.Errorf("xlsx: no worksheets found")
	}

	out := make([]xlsxSheet, 0, len(refs))
	for _, ref := range refs {
		f := zipFind(zr, ref.Path)
		if f == nil {
			warnings = append(warnings, fmt.Sprintf("xlsx: sheet %s file %s is missing", ref.Name, ref.Path))
			continue
		}
		rows, err := readSheetRows(f, shared)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("xlsx: sheet %s failed: %v", ref.Name, err))
			continue
		}
		out = append(out, xlsxSheet{Name: ref.Name, Table: rowsToTable(rows)})
	}
	return out, warnings, nil
}

func zipFind(zr *zip.ReadCloser, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func sheetFiles(zr *zip.ReadCloser) []*zip.File {
	files := []*zip.File{}
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			files = append(files, f)
		}
	}
	sort.Slice(files, func(i, j int) bool {
		return sheetNumber(files[i].Name) < sheetNumber(files[j].Name)
	})
	return files
}

func sheetNumber(name string) int {
	base := strings.TrimSuffix(strings.TrimPrefix(name, "xl/worksheets/sheet"), ".xml")
	n, err := strconv.Atoi(base)
	if err != nil {
		return 1 << 30
	}
	return n
}

func workbookSheets(zr *zip.ReadCloser) []xlsxSheetRef {
	rels := map[string]string{}
	if f := zipFind(zr, "xl/_rels/workbook.xml.rels"); f != nil {
		if rc, err := f.Open(); err == nil {
			dec := xml.NewDecoder(rc)
			for {
				tok, err := dec.Token()
				if err == io.EOF || err != nil {
					break
				}
				se, ok := tok.(xml.StartElement)
				if !ok || se.Name.Local != "Relationship" {
					continue
				}
				id, target := "", ""
				for _, a := range se.Attr {
					switch a.Name.Local {
					case "Id":
						id = a.Value
					case "Target":
						target = a.Value
					}
				}
				if id != "" && target != "" {
					rels[id] = normalizeTarget(target)
				}
			}
			rc.Close()
		}
	}

	refs := []xlsxSheetRef{}
	if f := zipFind(zr, "xl/workbook.xml"); f != nil {
		if rc, err := f.Open(); err == nil {
			dec := xml.NewDecoder(rc)
			for {
				tok, err := dec.Token()
				if err == io.EOF || err != nil {
					break
				}
				se, ok := tok.(xml.StartElement)
				if !ok || se.Name.Local != "sheet" {
					continue
				}
				name, rid := "", ""
				for _, a := range se.Attr {
					switch {
					case a.Name.Local == "name":
						name = a.Value
					case a.Name.Local == "id" || a.Name.Space != "" && strings.HasSuffix(a.Name.Local, "id"):
						rid = a.Value
					}
				}
				if rid == "" {
					continue
				}
				path, ok := rels[rid]
				if !ok {
					continue
				}
				if name == "" {
					name = fmt.Sprintf("Sheet%d", len(refs)+1)
				}
				refs = append(refs, xlsxSheetRef{Name: name, Path: path})
			}
			rc.Close()
		}
	}
	return refs
}

func normalizeTarget(target string) string {
	target = strings.TrimPrefix(target, "/")
	if !strings.HasPrefix(target, "xl/") {
		target = "xl/" + target
	}
	return target
}

func readSharedStrings(f *zip.File) ([]string, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	dec := xml.NewDecoder(rc)
	var out []string
	var cur strings.Builder
	inSI, inT := false, false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inSI = true
				cur.Reset()
			case "t":
				if inSI {
					inT = true
				}
			}
		case xml.CharData:
			if inT {
				cur.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inT = false
			case "si":
				out = append(out, cur.String())
				inSI = false
			}
		}
	}
	return out, nil
}

func readSheetRows(f *zip.File, shared []string) ([][]string, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	dec := xml.NewDecoder(rc)
	var rows [][]string
	var curRow []string
	var curVal strings.Builder
	var curType, curRef string
	inRow, inV, inT := false, false, false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				inRow = true
				curRow = []string{}
			case "c":
				curRef, curType = "", ""
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "r":
						curRef = a.Value
					case "t":
						curType = a.Value
					}
				}
				curVal.Reset()
			case "v":
				inV = true
			case "t":
				if inRow {
					inT = true
				}
			case "is":
				inT = true
			}
		case xml.CharData:
			if inV || inT {
				curVal.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "t":
				inT = false
			case "c":
				value := curVal.String()
				if curType == "s" {
					if idx, convErr := strconv.Atoi(strings.TrimSpace(value)); convErr == nil && idx >= 0 && idx < len(shared) {
						value = shared[idx]
					}
				}
				col := colFromRef(curRef)
				if col < 0 {
					col = len(curRow)
				}
				for len(curRow) <= col {
					curRow = append(curRow, "")
				}
				curRow[col] = value
			case "row":
				rows = append(rows, curRow)
				inRow = false
			}
		}
	}
	return rows, nil
}

func colFromRef(ref string) int {
	col := 0
	for _, r := range ref {
		if r >= 'A' && r <= 'Z' {
			col = col*26 + int(r-'A'+1)
			continue
		}
		break
	}
	if col == 0 {
		return -1
	}
	return col - 1
}

func rowsToTable(rows [][]string) *table {
	width := 0
	for _, r := range rows {
		if len(r) > width {
			width = len(r)
		}
	}
	if width == 0 {
		return &table{}
	}
	headers := make([]string, width)
	for i := range headers {
		if i < len(rows[0]) {
			headers[i] = strings.TrimSpace(rows[0][i])
		}
		if headers[i] == "" {
			headers[i] = fmt.Sprintf("column_%d", i+1)
		}
	}
	out := &table{Headers: headers}
	for _, r := range rows[1:] {
		row := make([]string, width)
		for i := range row {
			if i < len(r) {
				row[i] = r[i]
			}
		}
		out.Rows = append(out.Rows, row)
	}
	return out
}
