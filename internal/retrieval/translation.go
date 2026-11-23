package retrieval

import (
	"context"
	"strings"
	"unicode"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/index"
)

// textLanguage returns en/zh for text with a clear script signal. The empty
// language means "unknown"; callers must keep the original query in that case.
func textLanguage(text string) string {
	han, latin := 0, 0
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			han++
		case unicode.IsLetter(r):
			latin++
		}
	}
	if han == 0 && latin == 0 {
		return ""
	}
	if han >= latin {
		return config.KBLanguageZh
	}
	return config.KBLanguageEn
}

// corpusLanguage infers the dominant language from indexed chunk bodies.
// It samples chunks so an existing index needs no rebuild and no new metadata.
func corpusLanguage(chunks []index.Chunk) string {
	votes := map[string]float64{}
	for i := range chunks {
		lang := chunkLanguage(chunks[i])
		switch lang {
		case config.KBLanguageEn, config.KBLanguageZh:
			votes[lang] += 1
		}
	}
	en, zh := votes[config.KBLanguageEn], votes[config.KBLanguageZh]
	if en == 0 && zh == 0 {
		return ""
	}
	if zh >= en {
		return config.KBLanguageZh
	}
	return config.KBLanguageEn
}

// chunkLanguage uses explicit metadata when present, otherwise classifies the
// chunk by dominant script. Chunk-level votes avoid long code samples and
// identifier-heavy files skewing the whole corpus toward English.
func chunkLanguage(chunk index.Chunk) string {
	for _, key := range []string{"language", "lang"} {
		if lang := strings.ToLower(strings.TrimSpace(chunk.Metadata[key])); lang != "" {
			switch {
			case lang == "zh", strings.HasPrefix(lang, "zh-"), lang == "cmn", strings.HasPrefix(lang, "cmn-"):
				return config.KBLanguageZh
			case lang == "en", strings.HasPrefix(lang, "en-"):
				return config.KBLanguageEn
			}
		}
	}
	text := chunk.Body
	if text == "" {
		text = chunk.Text
	}
	text = truncateRunes(text, 400)
	han, latin := 0, 0
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			han++
		case unicode.IsLetter(r):
			latin++
		}
	}
	if han == 0 && latin == 0 {
		return ""
	}
	if han >= 3 && float64(han)/float64(han+latin) > 0.15 {
		return config.KBLanguageZh
	}
	if latin >= 10 {
		return config.KBLanguageEn
	}
	return ""
}

// translateQuery adapts a query for the lexical lane when query and corpus
// languages differ. Literals are never translated. Failure fails open.
func translateQuery(
	ctx context.Context,
	query string,
	from, to string,
	complete CompleteFunc,
) (string, error) {
	if complete == nil || from == "" || to == "" || from == to {
		return "", nil
	}
	target := "English"
	if to == config.KBLanguageZh {
		target = "Simplified Chinese"
	}
	system := "Translate the retrieval query into " + target + ". " +
		"Preserve product names, file paths, error codes, identifiers, version numbers and numbers verbatim. " +
		"Translate only natural-language concepts. Output only the translated query."
	out, err := callLLM(ctx, complete, system, query)
	if err != nil || out == "" {
		return "", err
	}
	cleaned := strings.TrimSpace(out)
	cleaned = strings.Trim(cleaned, "\"'`“”‘’")
	if cleaned == "" || cleaned == query {
		return "", nil
	}
	if len([]rune(cleaned)) > 1000 {
		return "", nil
	}
	return cleaned, nil
}
