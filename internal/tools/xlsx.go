package tools

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func readXLSXTable(path string) (*table, []string, error) {
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
	sheet := zipFindPrefix(zr, "xl/worksheets/sheet")
	if sheet == nil {
		return nil, warnings, fmt.Errorf("xlsx: no worksheet found")
	}
	rows, err := readSheetRows(sheet, shared)
	if err != nil {
		return nil, warnings, err
	}
	return rowsToTable(rows), warnings, nil
}

func zipFind(zr *zip.ReadCloser, name string) *zip.File {
	for _, f := range zr.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func zipFindPrefix(zr *zip.ReadCloser, prefix string) *zip.File {
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, prefix) && strings.HasSuffix(f.Name, ".xml") {
			return f
		}
	}
	return nil
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
