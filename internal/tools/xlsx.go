package tools

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"path"
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
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, err
	}
	defer zr.Close()

	warnings := []string{}
	refs, wbWarnings := workbookSheets(zr)
	warnings = append(warnings, wbWarnings...)
	if len(refs) == 0 {
		files := sheetFiles(zr)
		for i, f := range files {
			refs = append(refs, xlsxSheetRef{Name: fmt.Sprintf("Sheet%d", i+1), Path: f.Name})
		}
	}
	if len(refs) == 0 {
		return nil, warnings, fmt.Errorf("xlsx: no worksheets found")
	}
	target := refs[0]
	if sheetName != "" {
		found := false
		for _, ref := range refs {
			if strings.EqualFold(ref.Name, sheetName) {
				target = ref
				found = true
				break
			}
		}
		if !found {
			names := make([]string, 0, len(refs))
			for _, ref := range refs {
				names = append(names, ref.Name)
			}
			return nil, warnings, fmt.Errorf("xlsx: sheet %q not found, available: %s", sheetName, strings.Join(names, ", "))
		}
	}
	f := zipFind(zr, target.Path)
	if f == nil {
		return nil, warnings, fmt.Errorf("xlsx: sheet %s file %s is missing", target.Name, target.Path)
	}
	var shared []string
	if sf := zipFind(zr, "xl/sharedStrings.xml"); sf != nil {
		shared, err = readSharedStrings(sf)
		if err != nil {
			warnings = append(warnings, "xlsx: failed to read sharedStrings.xml: "+err.Error())
		}
	}
	styles, styleWarnings := readXLSXStyles(zr)
	warnings = append(warnings, styleWarnings...)
	rows, err := readSheetRows(f, shared, styles)
	if err != nil {
		return nil, warnings, err
	}
	return rowsToTable(rows), warnings, nil
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

	refs, wbWarnings := workbookSheets(zr)
	warnings = append(warnings, wbWarnings...)
	styles, styleWarnings := readXLSXStyles(zr)
	warnings = append(warnings, styleWarnings...)
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

func workbookSheets(zr *zip.ReadCloser) ([]xlsxSheetRef, []string) {
	warnings := []string{}
	rels := map[string]string{}
	if f := zipFind(zr, "xl/_rels/workbook.xml.rels"); f != nil {
		if rc, err := f.Open(); err == nil {
			dec := xml.NewDecoder(&maxBytesReader{r: rc, remaining: maxXLSXPartBytes})
			for {
				tok, err := dec.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					warnings = append(warnings, "xlsx: workbook.xml.rels could not be fully read: "+err.Error())
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
					if normalized := normalizeTarget(target); normalized != "" {
						rels[id] = normalized
					}
				}
			}
			rc.Close()
		}
	}

	refs := []xlsxSheetRef{}
	if f := zipFind(zr, "xl/workbook.xml"); f != nil {
		if rc, err := f.Open(); err == nil {
			dec := xml.NewDecoder(&maxBytesReader{r: rc, remaining: maxXLSXPartBytes})
			for {
				tok, err := dec.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					warnings = append(warnings, "xlsx: workbook.xml could not be fully read: "+err.Error())
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
				if !ok || path == "" {
					warnings = append(warnings, "worksheet relationship "+rid+" could not be resolved and was skipped")
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
	return refs, warnings
}

func normalizeTarget(target string) string {
	target = strings.TrimPrefix(target, "/")
	if !strings.HasPrefix(target, "xl/") {
		target = "xl/" + target
	}
	cleaned := path.Clean(target)
	if cleaned == "." || strings.HasPrefix(cleaned, "..") {
		return ""
	}
	return cleaned
}

func readSharedStrings(f *zip.File) ([]string, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	dec := xml.NewDecoder(&maxBytesReader{r: rc, remaining: maxXLSXSharedBytes})
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
		if len(out) > maxXLSXSharedStrings {
			return nil, fmt.Errorf("sharedStrings.xml exceeds %d entries", maxXLSXSharedStrings)
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
			case "is":
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
	dec := xml.NewDecoder(&maxBytesReader{r: rc, remaining: maxXLSXSheetBytes})
	var rows [][]string
	totalCells := 0
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
				inV, inT = false, false
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
				if (curType == "" || curType == "n") && curStyle >= 0 {
					if serial, ok := parseNumber(value); ok {
						switch {
						case styles.dateStyles[curStyle]:
							value = formatExcelDateTime(excelSerialToTime(serial, styles.date1904))
						case styles.elapsedStyles[curStyle]:
							value = formatExcelElapsed(serial)
						case styles.timeStyles[curStyle]:
							value = formatExcelTime(serial)
						}
					}
				}
				col := colFromRef(curRef)
				if col < 0 {
					col = len(curRow)
				}
				if col >= maxXLSXCols {
					continue
				}
				newCells := col + 1 - len(curRow)
				if newCells < 1 {
					newCells = 1
				}
				totalCells += newCells
				if totalCells > maxXLSXCells {
					return nil, fmt.Errorf("worksheet exceeds %d cells", maxXLSXCells)
				}
				for len(curRow) <= col {
					curRow = append(curRow, "")
				}
				curRow[col] = value
			case "row":
				if len(rows) >= maxXLSXRows {
					return nil, fmt.Errorf("worksheet exceeds %d rows", maxXLSXRows)
				}
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

// maxBytesReader fails the decode instead of turning a size overrun into a
// silent io.EOF.
type maxBytesReader struct {
	r         io.Reader
	remaining int64
}

func (m *maxBytesReader) Read(p []byte) (int, error) {
	if m.remaining <= 0 {
		return 0, fmt.Errorf("zip entry exceeds the configured size limit")
	}
	if int64(len(p)) > m.remaining {
		p = p[:m.remaining]
	}
	n, err := m.r.Read(p)
	m.remaining -= int64(n)
	return n, err
}

const (
	maxXLSXCols          = 16384
	maxXLSXRows          = 200000
	maxXLSXSharedBytes   = 64 << 20
	maxXLSXSharedStrings = 1000000
	maxXLSXSheetBytes    = 256 << 20
	maxXLSXCells         = 5000000
	maxXLSXPartBytes     = 64 << 20
)

type xlsxStyles struct {
	dateStyles    map[int]bool
	timeStyles    map[int]bool
	elapsedStyles map[int]bool
	date1904      bool
}

type fmtKind struct {
	date    bool
	time    bool
	elapsed bool
}

func readXLSXStyles(zr *zip.ReadCloser) (xlsxStyles, []string) {
	out := xlsxStyles{
		dateStyles:    map[int]bool{},
		timeStyles:    map[int]bool{},
		elapsedStyles: map[int]bool{},
	}
	warnings := []string{}
	if f := zipFind(zr, "xl/workbook.xml"); f != nil {
		if rc, err := f.Open(); err == nil {
			dec := xml.NewDecoder(&maxBytesReader{r: rc, remaining: maxXLSXPartBytes})
			for {
				tok, err := dec.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					warnings = append(warnings, "xlsx: workbook.xml could not be fully read: "+err.Error())
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
		return out, warnings
	}
	rc, err := f.Open()
	if err != nil {
		return out, warnings
	}
	defer rc.Close()
	dec := xml.NewDecoder(&maxBytesReader{r: rc, remaining: maxXLSXPartBytes})
	customFmt := map[int]fmtKind{}
	styleFormats := []int{}
	inCellXfs := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			warnings = append(warnings, "xlsx: styles.xml could not be fully read: "+err.Error())
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
				customFmt[id] = classifyFormatCode(code)
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
		kind, ok := customFmt[numFmtID]
		if !ok {
			kind = builtinFormatKind(numFmtID)
		}
		switch {
		case kind.date:
			out.dateStyles[idx] = true
		case kind.elapsed:
			out.elapsedStyles[idx] = true
		case kind.time:
			out.timeStyles[idx] = true
		}
	}
	return out, warnings
}

func builtinFormatKind(id int) fmtKind {
	switch {
	case id >= 14 && id <= 17, id == 22, id >= 27 && id <= 36, id >= 50 && id <= 58:
		return fmtKind{date: true}
	case id >= 18 && id <= 21:
		return fmtKind{time: true}
	case id == 45 || id == 47:
		// mm:ss and mmss.0 are clock times; only 46 ([h]:mm:ss) is elapsed.
		return fmtKind{time: true}
	case id == 46:
		return fmtKind{elapsed: true}
	}
	return fmtKind{}
}

// classifyFormatCode inspects a custom number format, skipping quoted literals,
// bracketed sections ([Red], [$-409]) and escaped characters (\d) so they are
// not mistaken for date/time tokens. Elapsed-time tokens such as [h] mark a
// duration format.
func classifyFormatCode(code string) fmtKind {
	runes := []rune(strings.ToLower(code))
	inQuote := false
	hasDate, hasTime, hasMonth := false, false, false
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '"':
			inQuote = !inQuote
		case inQuote:
		case r == '\\':
			i++
		case r == '[':
			j := i + 1
			for j < len(runes) && runes[j] != ']' {
				j++
			}
			if isElapsedToken(string(runes[i+1 : j])) {
				return fmtKind{elapsed: true}
			}
			i = j
		case r == 'y' || r == 'd':
			hasDate = true
		case r == 'h' || r == 's':
			hasTime = true
		case r == 'm':
			hasMonth = true
		}
	}
	if hasDate || (hasMonth && !hasTime) {
		return fmtKind{date: true}
	}
	if hasTime || hasMonth {
		return fmtKind{time: true}
	}
	return fmtKind{}
}

func isElapsedToken(section string) bool {
	section = strings.TrimSpace(section)
	if section == "" {
		return false
	}
	for _, r := range section {
		if r != 'h' && r != 'm' && r != 's' {
			return false
		}
	}
	return true
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

// formatExcelTime renders a time-only serial (fraction of a day) as a clock
// time instead of inventing an 1899/1904 date.
func formatExcelTime(serial float64) string {
	frac := serial - math.Floor(serial)
	total := int(math.Round(frac * 86400))
	total = ((total % 86400) + 86400) % 86400
	return fmt.Sprintf("%02d:%02d:%02d", total/3600, (total%3600)/60, total%60)
}

// formatExcelElapsed renders an elapsed-time serial as total hours:minutes:seconds,
// so durations such as [h]:mm:ss keep values >= 24h.
func formatExcelElapsed(serial float64) string {
	total := int(math.Round(serial * 86400))
	neg := total < 0
	if neg {
		total = -total
	}
	out := fmt.Sprintf("%d:%02d:%02d", total/3600, (total%3600)/60, total%60)
	if neg {
		return "-" + out
	}
	return out
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
