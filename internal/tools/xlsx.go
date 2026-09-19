package tools

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
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
	styles := readXLSXStyles(zr)
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
		rows, err := readSheetRows(f, shared, styles)
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

func readSheetRows(f *zip.File, shared []string, styles xlsxStyles) ([][]string, error) {
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
	curStyle := -1
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
				curStyle = -1
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "r":
						curRef = a.Value
					case "t":
						curType = a.Value
					case "s":
						if idx, convErr := strconv.Atoi(a.Value); convErr == nil {
							curStyle = idx
						}
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
				if (curType == "" || curType == "n") && curStyle >= 0 && styles.dateStyles[curStyle] {
					if serial, ok := parseNumber(value); ok {
						value = formatExcelDateTime(excelSerialToTime(serial, styles.date1904))
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

type xlsxStyles struct {
	dateStyles map[int]bool
	date1904   bool
}

func readXLSXStyles(zr *zip.ReadCloser) xlsxStyles {
	out := xlsxStyles{dateStyles: map[int]bool{}}
	if f := zipFind(zr, "xl/workbook.xml"); f != nil {
		if rc, err := f.Open(); err == nil {
			dec := xml.NewDecoder(rc)
			for {
				tok, err := dec.Token()
				if err == io.EOF || err != nil {
					break
				}
				se, ok := tok.(xml.StartElement)
				if !ok || se.Name.Local != "workbookPr" {
					continue
				}
				for _, a := range se.Attr {
					if a.Name.Local == "date1904" && (a.Value == "1" || a.Value == "true") {
						out.date1904 = true
					}
				}
			}
			rc.Close()
		}
	}
	f := zipFind(zr, "xl/styles.xml")
	if f == nil {
		return out
	}
	rc, err := f.Open()
	if err != nil {
		return out
	}
	defer rc.Close()
	dec := xml.NewDecoder(rc)
	customDateFmt := map[int]bool{}
	styleFormats := []int{}
	inCellXfs := false
	for {
		tok, err := dec.Token()
		if err == io.EOF || err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "numFmt":
				id, code := 0, ""
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "numFmtId":
						id, _ = strconv.Atoi(a.Value)
					case "formatCode":
						code = a.Value
					}
				}
				if isDateFormatCode(code) {
					customDateFmt[id] = true
				}
			case "cellXfs":
				inCellXfs = true
			case "xf":
				if inCellXfs {
					numFmtID := 0
					for _, a := range t.Attr {
						if a.Name.Local == "numFmtId" {
							numFmtID, _ = strconv.Atoi(a.Value)
						}
					}
					styleFormats = append(styleFormats, numFmtID)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "cellXfs" {
				inCellXfs = false
			}
		}
	}
	for idx, numFmtID := range styleFormats {
		if builtinDateFmt(numFmtID) || customDateFmt[numFmtID] {
			out.dateStyles[idx] = true
		}
	}
	return out
}

func builtinDateFmt(id int) bool {
	switch {
	case id >= 14 && id <= 22:
		return true
	case id >= 27 && id <= 36:
		return true
	case id >= 45 && id <= 47:
		return true
	case id >= 50 && id <= 58:
		return true
	}
	return false
}

func isDateFormatCode(code string) bool {
	inQuote := false
	hasDate, hasTime := false, false
	for _, r := range strings.ToLower(code) {
		if r == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			continue
		}
		switch r {
		case 'y', 'd', 'm':
			hasDate = true
		case 'h', 's':
			hasTime = true
		}
	}
	return hasDate || hasTime
}

func excelSerialToTime(serial float64, date1904 bool) time.Time {
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	if date1904 {
		base = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
	} else if serial < 60 {
		base = time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC)
	}
	days := math.Floor(serial)
	frac := serial - days
	return base.AddDate(0, 0, int(days)).Add(time.Duration(frac * 24 * float64(time.Hour)))
}

func formatExcelDateTime(t time.Time) string {
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 {
		return t.Format("2006-01-02")
	}
	return t.Format("2006-01-02 15:04:05")
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
