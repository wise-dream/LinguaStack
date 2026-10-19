package translation

import (
	"slices"
	"strings"
)

// languageNames — коды ISO 639-1 и английские названия для промпта
// (модели надёжнее следуют инструкциям на английском). Список не
// ограничивает модель: возможности конкретной модели — в ModelCapabilities
// (ТЗ §5.2).
var languageNames = map[string]string{
	"ar": "Arabic", "bg": "Bulgarian", "cs": "Czech", "da": "Danish", "de": "German", "el": "Greek",
	"en": "English", "es": "Spanish", "et": "Estonian", "fa": "Persian", "fi": "Finnish", "fr": "French",
	"he": "Hebrew", "hi": "Hindi", "hr": "Croatian", "hu": "Hungarian", "hy": "Armenian", "id": "Indonesian",
	"it": "Italian", "ja": "Japanese", "ka": "Georgian", "kk": "Kazakh", "ko": "Korean", "lt": "Lithuanian",
	"lv": "Latvian", "nl": "Dutch", "no": "Norwegian", "pl": "Polish", "pt": "Portuguese", "ro": "Romanian",
	"ru": "Russian", "sk": "Slovak", "sl": "Slovenian", "sr": "Serbian", "sv": "Swedish", "th": "Thai",
	"tr": "Turkish", "uk": "Ukrainian", "uz": "Uzbek", "vi": "Vietnamese", "zh": "Chinese",
}

// LanguageName — название языка по коду.
func LanguageName(code string) (string, bool) {
	name, ok := languageNames[code]
	return name, ok
}

// IsKnownLanguage — поддерживается ли код.
func IsKnownLanguage(code string) bool {
	_, ok := languageNames[code]
	return ok
}

// Language — язык из списка поддерживаемых: код ISO 639-1 и английское
// название.
type Language struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Languages — все поддерживаемые языки по возрастанию кода.
func Languages() []Language {
	out := make([]Language, 0, len(languageNames))
	for code, name := range languageNames {
		out = append(out, Language{Code: code, Name: name})
	}
	slices.SortFunc(out, func(a, b Language) int { return strings.Compare(a.Code, b.Code) })
	return out
}
