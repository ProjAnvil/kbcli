package i18n

import (
	"sort"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/config"
)

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Lang
	}{
		{"", LangEn},
		{"en", LangEn},
		{"EN", LangEn},
		{"zh", LangZh},
		{"ZH", LangZh},
		{"zh-CN", LangZh},
		{"zh-Hans", LangZh},
		{"中文", LangZh},
		{"cn", LangZh},
		{"fr", LangEn},
		{"  zh  ", LangZh},
	}
	for _, tc := range cases {
		if got := Parse(tc.in); got != tc.want {
			t.Fatalf("Parse(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCatalogKeyParity(t *testing.T) {
	enKeys := make([]string, 0, len(catalogEn))
	zhKeys := make([]string, 0, len(catalogZh))
	for k := range catalogEn {
		enKeys = append(enKeys, k)
	}
	for k := range catalogZh {
		zhKeys = append(zhKeys, k)
	}
	sort.Strings(enKeys)
	sort.Strings(zhKeys)
	if len(enKeys) != len(zhKeys) {
		t.Fatalf("key count: en=%d zh=%d", len(enKeys), len(zhKeys))
	}
	for i := range enKeys {
		if enKeys[i] != zhKeys[i] {
			t.Fatalf("key mismatch at %d: en=%q zh=%q", i, enKeys[i], zhKeys[i])
		}
	}
	if keys := Keys(); len(keys) != len(enKeys) {
		t.Fatalf("Keys() = %d, want %d", len(keys), len(enKeys))
	}
}

func TestTAndTf(t *testing.T) {
	if got := LangEn.T("evidence.header"); got != "EVIDENCE · recall ranking" {
		t.Fatalf("en header = %q", got)
	}
	if got := LangZh.T("evidence.header"); !strings.Contains(got, "召回") {
		t.Fatalf("zh header = %q", got)
	}
	if got := LangZh.T("does.not.exist"); got != "does.not.exist" {
		t.Fatalf("unknown key = %q", got)
	}
	if got := LangEn.Tf("app.header", "/docs", "1.2.3"); got != "kbcli · /docs · v1.2.3" {
		t.Fatalf("Tf = %q", got)
	}
	if got := LangZh.Tf("evidence.page", 3); got != "第 3 页" {
		t.Fatalf("zh page = %q", got)
	}
}

func TestEnglishMatchesExistingUI(t *testing.T) {
	if got := LangEn.T("chat.help"); got != "enter send · :settings / :reindex / :update / :help / :clear / :q · tab · ctrl+] evidence · pgup/pgdn · q/ctrl+c quit" {
		t.Fatalf("English help line drifted: %q", got)
	}
	if got := LangEn.T("settings.help"); got != catalogEn["settings.help"] {
		t.Fatal("settings help changed")
	}
	if got := LangEn.T("settings.stale"); !strings.Contains(got, "INDEX STALE") || !strings.Contains(got, "press r to reindex") {
		t.Fatalf("English stale banner drifted: %q", got)
	}
}

func TestEveryConfigFieldHasBilingualLabel(t *testing.T) {
	fields := config.FieldKeys()
	if len(fields) < 30 {
		t.Fatalf("only %d config fields", len(fields))
	}
	different := 0
	for _, key := range fields {
		k := "field." + key + ".label"
		en, okEn := catalogEn[k]
		zh, okZh := catalogZh[k]
		if !okEn || !okZh || en == "" || zh == "" {
			t.Fatalf("missing label for %q", key)
		}
		if en != zh {
			different++
		}
	}
	if different < 20 {
		t.Fatalf("only %d labels are actually translated", different)
	}
	for _, f := range config.Fields() {
		k := "group." + f.Group
		if _, ok := catalogEn[k]; !ok {
			t.Fatalf("missing en group key %q", k)
		}
		if _, ok := catalogZh[k]; !ok {
			t.Fatalf("missing zh group key %q", k)
		}
	}
}

func TestPromptKeysAndChineseAnaphora(t *testing.T) {
	for _, key := range []string{
		"prompt.system_en", "prompt.system_zh", "prompt.question", "prompt.evidence",
		"prompt.facts", "prompt.summary", "prompt.recent",
		"condense.system_en", "condense.system_zh",
		"facts.system_en", "facts.system_zh",
		"compact.system_en", "compact.system_zh",
		"fallback.llm_unavailable", "fallback.no_evidence",
	} {
		if LangEn.T(key) == key || LangZh.T(key) == key {
			t.Fatalf("prompt key %q is missing", key)
		}
	}
	if !strings.Contains(LangZh.T("condense.system_zh"), "它") {
		t.Fatal("Chinese condense prompt does not mention 它")
	}
	if !strings.Contains(LangEn.T("condense.system_en"), "PASS") {
		t.Fatal("English condense prompt does not define the PASS gate")
	}
}

func TestFieldLabelFallback(t *testing.T) {
	if got := LangZh.FieldLabel("chunk_size"); got != "分块大小" {
		t.Fatalf("zh field label = %q", got)
	}
	if got := LangEn.FieldLabel("chunk_size"); got != "Chunk size" {
		t.Fatalf("en field label = %q", got)
	}
	if got := LangZh.FieldLabel("not_a_field"); got != "not_a_field" {
		t.Fatalf("unknown field label = %q", got)
	}
	if got := LangZh.GroupLabel("Retrieval"); got != "检索" {
		t.Fatalf("zh group label = %q", got)
	}
}
