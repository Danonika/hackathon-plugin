package tools

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

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
				t, warns, terr := readAnyTable(full, "")
				warnings = append(warnings, warns...)
				if terr != nil {
					warnings = append(warnings, terr.Error())
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
				sheets, warns, terr := readXLSXSheets(full)
				warnings = append(warnings, warns...)
				if terr != nil {
					warnings = append(warnings, terr.Error())
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
					b, rerr := readCapped(full, maxTextInspectBytes)
					if rerr != nil {
						warnings = append(warnings, "could not read text content: "+rerr.Error())
					} else {
						data["lines"] = strings.Count(strings.TrimSuffix(string(b), "\n"), "\n") + 1
						data["chars"] = len([]rune(string(b)))
						if int64(len(b)) >= maxTextInspectBytes {
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
