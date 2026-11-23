package agent

import (
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/i18n"
)

func TestSystemPromptSections(t *testing.T) {
	en := systemPrompt(i18n.LangEn, answerDirectiveEn)
	for _, want := range []string{
		"Instruction priority:",
		"Task protocol:",
		"Source priority when claims conflict:",
		"Verification before the final answer:",
		"Language:",
		"Tools (read only):",
		answerDirectiveEn,
	} {
		if !strings.Contains(en, want) {
			t.Fatalf("English prompt missing %q", want)
		}
	}

	zh := systemPrompt(i18n.LangZh, answerDirectiveZh)
	for _, want := range []string{
		"指令优先级：",
		"任务流程：",
		"来源冲突时的优先级：",
		"回答前自检：",
		"语言：",
		"只读工具：",
		answerDirectiveZh,
	} {
		if !strings.Contains(zh, want) {
			t.Fatalf("Chinese prompt missing %q", want)
		}
	}
}

func TestSystemPromptStablePrefix(t *testing.T) {
	first := systemPrompt(i18n.LangEn, "Answer in English.")
	second := systemPrompt(i18n.LangEn, "Answer in French.")
	tailA := strings.TrimSpace(first[len(systemPrefixEn):])
	tailB := strings.TrimSpace(second[len(systemPrefixEn):])
	if !strings.HasPrefix(tailA, "Answer in English.") {
		t.Fatalf("English tail = %q", tailA)
	}
	if !strings.HasPrefix(tailB, "Answer in French.") {
		t.Fatalf("alternate tail = %q", tailB)
	}
	if !strings.HasPrefix(first, systemPrefixEn) || !strings.HasPrefix(second, systemPrefixEn) {
		t.Fatal("dynamic directive changed the stable prompt prefix")
	}
}

func TestSystemPromptDoesNotForceRecitation(t *testing.T) {
	if strings.Contains(systemPrefixEn, "Answer only from") {
		t.Fatal("English prompt must allow synthesis")
	}
	if strings.Contains(systemPrefixZh, "只依据提供的证据回答") {
		t.Fatal("Chinese prompt must allow synthesis")
	}
}
