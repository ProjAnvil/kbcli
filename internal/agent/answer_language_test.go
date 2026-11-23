package agent

import (
	"testing"

	"github.com/ProjAnvil/kbcli/internal/i18n"
)

func TestQuestionLanguage(t *testing.T) {
	tests := []struct {
		name     string
		question string
		fallback i18n.Lang
		want     i18n.Lang
	}{
		{"english question", "What is the retry policy?", i18n.LangZh, i18n.LangEn},
		{"chinese question", "重试策略是什么？", i18n.LangEn, i18n.LangZh},
		{"english term in chinese question", "重试策略 retry 是什么？", i18n.LangEn, i18n.LangZh},
		{"chinese term in english question", "What is 重试策略?", i18n.LangZh, i18n.LangEn},
		{"no letters uses fallback", "123?", i18n.LangZh, i18n.LangZh},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := questionLanguage(tt.question, tt.fallback); got != tt.want {
				t.Fatalf("questionLanguage = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAnswerDirective(t *testing.T) {
	if got := answerDirective(i18n.LangEn); got != answerDirectiveEn {
		t.Fatalf("english directive = %q", got)
	}
	if got := answerDirective(i18n.LangZh); got != answerDirectiveZh {
		t.Fatalf("chinese directive = %q", got)
	}
}

func TestDetectQuestionLanguageExplicitSwitch(t *testing.T) {
	if lang, ok := detectQuestionLanguage("请用英文回答这个问题"); !ok || lang != i18n.LangEn {
		t.Fatalf("explicit english switch = %q, %t", lang, ok)
	}
	if lang, ok := detectQuestionLanguage("Please answer in Chinese"); !ok || lang != i18n.LangZh {
		t.Fatalf("explicit chinese switch = %q, %t", lang, ok)
	}
	if lang, ok := detectQuestionLanguage("123?"); ok || lang != "" {
		t.Fatalf("no-signal language = %q, %t", lang, ok)
	}
}

func TestRunnerLanguageSticksUntilSwitch(t *testing.T) {
	r := &runner{activeLang: i18n.LangEn}
	if got := r.answerLanguage("123?", i18n.LangEn); got != i18n.LangEn {
		t.Fatalf("no-signal language = %q, want en", got)
	}
	if got := r.answerLanguage("重试策略是什么？", i18n.LangEn); got != i18n.LangZh {
		t.Fatalf("chinese question = %q, want zh", got)
	}
	if got := r.answerLanguage("and what about backoff?", i18n.LangEn); got != i18n.LangEn {
		t.Fatalf("english question = %q, want en", got)
	}
	if got := r.answerLanguage("请继续", i18n.LangEn); got != i18n.LangZh {
		t.Fatalf("switched language = %q, want zh", got)
	}
	if got := r.answerLanguage("why?", i18n.LangEn); got != i18n.LangEn {
		t.Fatalf("explicit english cue = %q, want en", got)
	}
}
