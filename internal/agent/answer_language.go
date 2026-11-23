package agent

import (
	"strings"
	"unicode"

	"github.com/ProjAnvil/kbcli/internal/i18n"
)

const (
	answerDirectiveEn = "Answer in English."
	answerDirectiveZh = "请用中文回答。"
)

var (
	englishRequests = []string{
		"answer in english", "respond in english", "reply in english",
		"speak english", "use english", "用英文", "用英语", "英文回答",
	}
	chineseRequests = []string{
		"answer in chinese", "respond in chinese", "reply in chinese",
		"speak chinese", "use chinese", "用中文", "用汉语", "中文回答",
	}
	englishCues = []string{
		"what ", "why ", "how ", "when ", "where ", "which ", "who ",
		"can you", "could you", "does ", "do ", "is ", "are ", "explain ",
		"describe ", "list ", "give ", "tell ", "compare ",
	}
	chineseCues = []string{
		"什么", "怎么", "如何", "为什么", "是否", "哪些", "哪个",
		"吗", "呢", "请", "介绍", "解释", "分析", "比较", "列出",
	}
)

// detectQuestionLanguage reports an explicit language choice or a clear signal
// from the question text. Mixed technical terms alone are not a clear signal.
func detectQuestionLanguage(question string) (i18n.Lang, bool) {
	lower := strings.ToLower(strings.TrimSpace(question))
	for _, phrase := range englishRequests {
		if strings.Contains(lower, phrase) {
			return i18n.LangEn, true
		}
	}
	for _, phrase := range chineseRequests {
		if strings.Contains(lower, phrase) {
			return i18n.LangZh, true
		}
	}

	han, latin := 0, 0
	var firstLetter rune
	for _, r := range question {
		if !unicode.IsLetter(r) {
			continue
		}
		if firstLetter == 0 {
			firstLetter = r
		}
		if unicode.Is(unicode.Han, r) {
			han++
		} else {
			latin++
		}
	}
	if han > 0 && latin > 0 {
		enCue, zhCue := hasAny(lower, englishCues), hasAny(question, chineseCues)
		if enCue && !zhCue {
			return i18n.LangEn, true
		}
		if zhCue && !enCue {
			return i18n.LangZh, true
		}
		if enCue != zhCue {
			if enCue {
				return i18n.LangEn, true
			}
			return i18n.LangZh, true
		}
		// No question marker. The leading script is the best available clue.
		if firstLetter != 0 && unicode.Is(unicode.Han, firstLetter) {
			return i18n.LangZh, true
		}
		if firstLetter != 0 {
			return i18n.LangEn, true
		}
		return "", false
	}
	if han > 0 {
		return i18n.LangZh, true
	}
	if latin > 0 {
		return i18n.LangEn, true
	}
	return "", false
}

func hasAny(s string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// questionLanguage is kept for callers that only need a one-shot decision.
// The configured language wins when the question gives no usable signal.
func questionLanguage(question string, fallback i18n.Lang) i18n.Lang {
	if lang, ok := detectQuestionLanguage(question); ok {
		return lang
	}
	return fallback
}

func answerDirective(lang i18n.Lang) string {
	if lang == i18n.LangZh {
		return answerDirectiveZh
	}
	return answerDirectiveEn
}
