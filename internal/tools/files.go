package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"hackathonkit.local/agentkit/internal/mcp"
)

const maxTextInspectBytes = 8 << 20

func readCapped(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit))
}

func registerFiles(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"files.describe",
		"Describe an input file: type, size, rows, columns, headers and timestamp presence. Use it to plan analysis.",
		map[string]any{
			"path": strProp("Relative path under data/input"),
		},
		[]string{"path"},
		func(ctx context.Context, args map[string]any) (any, error) {
			path := argString(args, "path")
			if path == "" {
				return mcp.Fail(trace(ctx), "path is required"), nil
			}
			full, err := d.Store.InputPath(path)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			st, err := os.Stat(full)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(full), "."))
			data := map[string]any{
				"path":       path,
				"extension":  ext,
				"size_bytes": st.Size(),
			}
			warnings := []string{}
			switch ext {
			case "csv", "tsv":
				if st.Size() == 0 {
					data["rows"] = 0
					data["columns"] = 0
					data["headers"] = []string{}
					data["has_timestamp"] = false
					warnings = append(warnings, "empty table")
					break
				}
				if st.Size() > maxTextInspectBytes {
					warnings = append(warnings, fmt.Sprintf("table is %d bytes; skipping full parse (limit %d)", st.Size(), maxTextInspectBytes))
					break
				}
				t, warns, terr := readAnyTable(ctx, full, "")
				warnings = append(warnings, warns...)
				if terr != nil {
					return mcp.Partial(trace(ctx), data, append(warnings, terr.Error())...), nil
				} else {
					hasTS := false
					for _, h := range t.Headers {
						if timestampHeader(h) {
							hasTS = true
						}
					}
					data["rows"] = len(t.Rows)
					data["columns"] = len(t.Headers)
					data["headers"] = t.Headers
					data["has_timestamp"] = hasTS
				}
			case "xlsx":
				if st.Size() > maxTextInspectBytes {
					warnings = append(warnings, fmt.Sprintf("workbook is %d bytes; skipping full parse (limit %d)", st.Size(), maxTextInspectBytes))
					break
				}
				sheets, warns, terr := readXLSXSheets(ctx, full)
				warnings = append(warnings, warns...)
				if terr != nil {
					return mcp.Partial(trace(ctx), data, append(warnings, terr.Error())...), nil
				} else {
					names := make([]string, 0, len(sheets))
					for _, s := range sheets {
						names = append(names, s.Name)
					}
					data["sheets"] = names
					if len(sheets) > 0 {
						t := sheets[0].Table
						hasTS := false
						for _, h := range t.Headers {
							if timestampHeader(h) {
								hasTS = true
							}
						}
						data["rows"] = len(t.Rows)
						data["columns"] = len(t.Headers)
						data["headers"] = t.Headers
						data["has_timestamp"] = hasTS
						data["first_sheet"] = sheets[0].Name
					}
				}
			case "xls":
				warnings = append(warnings, "legacy .xls (BIFF) workbooks are not supported; convert the file to .xlsx or .csv first")
			default:
				if isTextExt(ext) {
					b, rerr := readCapped(full, maxTextInspectBytes+1)
					if rerr != nil {
						warnings = append(warnings, "could not read text content: "+rerr.Error())
					} else {
						s := string(b)
						if len(b) == 0 {
							data["lines"] = 0
						} else {
							data["lines"] = strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
						}
						data["chars"] = utf8.RuneCount(b)
						if int64(len(b)) > maxTextInspectBytes {
							warnings = append(warnings, "text content truncated for inspection")
						}
					}
				} else {
					warnings = append(warnings, "binary or unsupported file type: only metadata is reported")
				}
			}
			env := mcp.OK(trace(ctx), data)
			if len(warnings) > 0 {
				env = env.WithWarnings(warnings...)
			}
			return env, nil
		},
	))
}
