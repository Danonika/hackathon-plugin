package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"hackathonkit.local/agentkit/internal/mcp"
)

type indexChunk struct {
	ID    int            `json:"id"`
	Text  string         `json:"text"`
	Terms map[string]int `json:"terms"`
}

type indexDoc struct {
	Source     string       `json:"source"`
	ArtifactID string       `json:"artifact_id,omitempty"`
	Corpus     string       `json:"corpus,omitempty"`
	CreatedAt  string       `json:"created_at"`
	Chunks     []indexChunk `json:"chunks"`
}

var stopwords = map[string]bool{
	"и": true, "в": true, "во": true, "на": true, "с": true, "со": true, "по": true, "для": true,
	"не": true, "что": true, "как": true, "это": true, "из": true, "или": true, "а": true, "но": true,
	"the": true, "and": true, "for": true, "with": true, "that": true, "this": true, "from": true,
	"are": true, "was": true, "were": true, "you": true, "your": true,
}

func tokenize(text string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if utf8.RuneCountInString(cur.String()) >= 2 {
			token := cur.String()
			if !stopwords[token] {
				out = append(out, token)
			}
		}
		cur.Reset()
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func chunkText(text string, size, overlap int) []string {
	if size <= 0 || size > 20000 {
		size = 800
	}
	// Keep a real stride: an overlap of size-1 would make step 1 and turn
	// chunking into O(n^2).
	if overlap < 0 || overlap >= size {
		overlap = size / 8
	} else if overlap > size/2 {
		overlap = size / 2
	}
	r := []rune(text)
	if len(r) == 0 {
		return nil
	}
	var out []string
	step := size - overlap
	if step <= 0 {
		step = size
	}
	for start := 0; start < len(r); start += step {
		end := start + size
		if end > len(r) {
			end = len(r)
		}
		out = append(out, string(r[start:end]))
		if end == len(r) {
			break
		}
	}
	return out
}

func registerKnowledge(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"knowledge.index",
		"Chunk and index text from an input file or a stored artifact so it can be found by knowledge.search.",
		map[string]any{
			"path":        strProp("Relative path under data/input (alternative to artifact_id)"),
			"artifact_id": strProp("Stored artifact id from documents.ingest (alternative to path)"),
			"corpus":      strProp("Optional corpus label used to scope later searches"),
			"chunk_chars": numProp("Chunk size in characters (default 800)"),
			"overlap":     numProp("Chunk overlap in characters (default 100)"),
		},
		nil,
		func(ctx context.Context, args map[string]any) (any, error) {
			source := argString(args, "path")
			artifactID := argString(args, "artifact_id")
			var text string
			var warnings []string
			if artifactID != "" {
				raw, err := d.Store.ReadArtifact(artifactID)
				if err != nil {
					return mcp.Fail(trace(ctx), err.Error()), nil
				}
				text = string(raw)
				source = artifactID
			} else if source != "" {
				full, err := d.Store.InputPath(source)
				if err != nil {
					return mcp.Fail(trace(ctx), err.Error()), nil
				}
				extracted, warns, err := extractText(full)
				if err != nil {
					return mcp.Fail(trace(ctx), err.Error()), nil
				}
				text = extracted
				warnings = warns
			} else {
				return mcp.Fail(trace(ctx), "either path or artifact_id is required"), nil
			}
			chunkSize := argInt(args, "chunk_chars", 800)
			if chunkSize <= 0 || chunkSize > 20000 {
				chunkSize = 800
			}
			chunkOverlap := argInt(args, "overlap", 100)
			if chunkOverlap < 0 || chunkOverlap >= chunkSize {
				chunkOverlap = chunkSize / 8
			} else if chunkOverlap > chunkSize/2 {
				chunkOverlap = chunkSize / 2
			}
			chunks := chunkText(text, chunkSize, chunkOverlap)
			if len(chunks) == 0 {
				return mcp.Fail(trace(ctx), "no text to index"), nil
			}
			corpus := argString(args, "corpus")
			doc := indexDoc{Source: source, ArtifactID: artifactID, Corpus: corpus, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
			for i, chunk := range chunks {
				terms := map[string]int{}
				for _, tok := range tokenize(chunk) {
					terms[tok]++
				}
				doc.Chunks = append(doc.Chunks, indexChunk{ID: i, Text: chunk, Terms: terms})
			}
			payload, err := json.Marshal(doc)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			base := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
			indexKey := source
			if corpus != "" {
				indexKey = source + "\x00" + corpus
			}
			indexID, werr := d.Store.WriteIndex(
				fmt.Sprintf("knowledge/%s-%s.json", slugify(base), shortHash(indexKey)),
				payload,
			)
			if werr != nil {
				return mcp.Fail(trace(ctx), werr.Error()), nil
			}
			env := mcp.OK(trace(ctx), map[string]any{
				"index_id":    indexID,
				"source":      source,
				"chunk_count": len(doc.Chunks),
			})
			if len(warnings) > 0 {
				env = env.WithWarnings(warnings...)
			}
			return env, nil
		},
	))

	reg.Register(tool(
		"knowledge.search",
		"Search previously indexed text (TF-IDF style) and return grounded chunks with sources.",
		map[string]any{
			"query":      strProp("Search query"),
			"limit":      numProp("Maximum hits (default 5)"),
			"corpus":     strProp("Restrict the search to one corpus label"),
			"index_id":   strProp("Restrict the search to a single index_id returned by knowledge.index"),
			"index_ids":  arrProp("Restrict the search to several index_ids", map[string]any{"type": "string"}),
			"text_chars": numProp("Characters of chunk text to return per hit (default 300, max 4000)"),
		},
		[]string{"query"},
		func(ctx context.Context, args map[string]any) (any, error) {
			query := argString(args, "query")
			if query == "" {
				return mcp.Fail(trace(ctx), "query is required"), nil
			}
			limit := argInt(args, "limit", 5)
			if limit <= 0 || limit > 50 {
				limit = 5
			}
			textChars := argInt(args, "text_chars", 300)
			if textChars <= 0 || textChars > 4000 {
				textChars = 300
			}
			corpus := argString(args, "corpus")
			warnings := []string{}
			allowed := map[string]bool{}
			for _, id := range argStringSlice(args, "index_ids") {
				allowed[id] = true
			}
			if id := argString(args, "index_id"); id != "" {
				allowed[id] = true
			}
			indexFiles, err := d.Store.ListIndexes()
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			if len(indexFiles) == 0 {
				return mcp.Partial(trace(ctx), map[string]any{"hits": []any{}, "index_count": 0},
					"knowledge index is empty; run knowledge.index first"), nil
			}
			type loadedDoc struct {
				ID  string
				Doc indexDoc
			}
			docs := []loadedDoc{}
			for _, name := range indexFiles {
				if len(allowed) > 0 && !allowed[name] {
					continue
				}
				raw, err := d.Store.ReadIndex(name)
				if err != nil {
					warnings = append(warnings, "index "+name+" is unreadable and was skipped")
					continue
				}
				var doc indexDoc
				if json.Unmarshal(raw, &doc) != nil {
					warnings = append(warnings, "index "+name+" is corrupt and was skipped")
					continue
				}
				if corpus != "" && doc.Corpus != corpus {
					continue
				}
				docs = append(docs, loadedDoc{ID: name, Doc: doc})
			}
			if len(docs) == 0 {
				return mcp.Partial(trace(ctx), map[string]any{"hits": []any{}, "index_count": 0},
					"no index matched the requested scope; index the corpus first"), nil
			}
			totalChunks := 0
			df := map[string]int{}
			for _, ld := range docs {
				for _, chunk := range ld.Doc.Chunks {
					totalChunks++
					for term := range chunk.Terms {
						df[term]++
					}
				}
			}
			queryTerms := map[string]bool{}
			for _, tok := range tokenize(query) {
				queryTerms[tok] = true
			}
			type hit struct {
				Source     string  `json:"source"`
				ArtifactID string  `json:"artifact_id,omitempty"`
				IndexID    string  `json:"index_id"`
				ChunkID    int     `json:"chunk_id"`
				Score      float64 `json:"score"`
				Snippet    string  `json:"snippet"`
				Text       string  `json:"text"`
			}
			hits := []hit{}
			for _, ld := range docs {
				for _, chunk := range ld.Doc.Chunks {
					score := 0.0
					for term := range queryTerms {
						tf := chunk.Terms[term]
						if tf == 0 {
							continue
						}
						idf := math.Log(1 + float64(totalChunks)/float64(maxInt(df[term], 1)))
						score += (1 + math.Log(float64(tf))) * idf
					}
					if score > 0 {
						hits = append(hits, hit{
							Source:     ld.Doc.Source,
							ArtifactID: ld.Doc.ArtifactID,
							IndexID:    ld.ID,
							ChunkID:    chunk.ID,
							Score:      float64(int(score*1000+0.5)) / 1000,
							Snippet:    snippet(chunk.Text, 300),
							Text:       snippet(chunk.Text, textChars),
						})
					}
				}
			}
			sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
			if len(hits) > limit {
				hits = hits[:limit]
			}
			env := mcp.OK(trace(ctx), map[string]any{
				"hits":         hits,
				"index_count":  len(docs),
				"total_chunks": totalChunks,
				"matched":      len(hits),
			})
			for _, h := range hits {
				artifactID := h.ArtifactID
				if artifactID == "" {
					artifactID = h.IndexID
				}
				env = env.WithSources(mcp.Source{
					ArtifactID: artifactID,
					Block:      fmt.Sprintf("chunk:%d", h.ChunkID),
				})
			}
			if len(hits) == 0 {
				env = env.WithWarnings("no matches for the query")
			}
			if len(warnings) > 0 {
				env = env.WithWarnings(warnings...)
			}
			return env, nil
		},
	))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
