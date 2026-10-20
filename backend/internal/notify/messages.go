package notify

import (
	"fmt"
	"html"
	"strings"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/telegram"
)

// Тексты уведомлений (дополнение §17): название, язык, счётчики и ссылки —
// без текста книги.

// Сколько названий глав перечислять в склеенном сообщении и сколько
// символов названий оставлять: сообщение не длиннее предела Telegram.
const (
	shownChapters = 3
	titleLimit    = 100
	chapterLimit  = 80
)

type projectInfo struct {
	project models.TranslationProject
	title   string
	doc     *cdm.Document // для названий глав; nil — не загружен
	base    string        // адрес веб-приложения без «/» в конце; "" — без ссылок
}

func (i *projectInfo) head() string {
	return "<b>" + html.EscapeString(telegram.Clip(i.title, titleLimit)) + "</b> → " + html.EscapeString(telegram.LanguageName(i.project.TargetLang))
}

func (i *projectInfo) link(path, text string) string {
	if i.base == "" {
		return ""
	}
	return "\n" + telegram.Link(i.base+path, text)
}

func (i *projectInfo) projectPage() string {
	return i.link("/translations/"+i.project.ID.String(), "Открыть перевод")
}

func (i *projectInfo) readerPage(chapter *int) string {
	path := "/read/" + i.project.BookID.String() + "?t=" + i.project.ID.String()
	text := "Читать"
	if chapter != nil {
		path += fmt.Sprintf("&ch=%d", *chapter)
		text = "Читать главу"
	}
	return i.link(path, text)
}

func (i *projectInfo) chapterTitle(index int) string {
	if i.doc != nil {
		if ch := findChapter(i.doc.Chapters, index); ch != nil && ch.Title != "" {
			return ch.Title
		}
	}
	return fmt.Sprintf("глава %d", index+1)
}

func findChapter(chs []cdm.Chapter, index int) *cdm.Chapter {
	for i := range chs {
		if chs[i].Index == index {
			return &chs[i]
		}
		if ch := findChapter(chs[i].Children, index); ch != nil {
			return ch
		}
	}
	return nil
}

func (i *projectInfo) progress() string {
	return fmt.Sprintf("Переведено %d из %d.", i.project.SegmentsDone, i.project.SegmentsTotal)
}

// chaptersText — «готова глава «A»» или «готовы главы «A», «B», «C» и ещё 2».
func chaptersText(i *projectInfo, indexes []int) string {
	quoted := make([]string, 0, shownChapters)
	for _, idx := range indexes[:min(len(indexes), shownChapters)] {
		quoted = append(quoted, "«"+html.EscapeString(telegram.Clip(i.chapterTitle(idx), chapterLimit))+"»")
	}
	var what string
	if len(indexes) == 1 {
		what = "готова глава " + quoted[0]
	} else {
		what = "готовы главы " + strings.Join(quoted, ", ")
		if rest := len(indexes) - len(quoted); rest > 0 {
			what += fmt.Sprintf(" и ещё %d", rest)
		}
	}
	first := indexes[0]
	return i.head() + ": " + what + ". " + i.progress() + i.readerPage(&first)
}

func completedText(i *projectInfo) string {
	p := i.project
	text := fmt.Sprintf("Перевод %s на %s готов: %d из %d фрагментов", "<b>"+html.EscapeString(telegram.Clip(i.title, titleLimit))+"</b>",
		html.EscapeString(telegram.LanguageName(p.TargetLang)), p.SegmentsDone, p.SegmentsTotal)
	if p.WarningsCount > 0 {
		text += fmt.Sprintf(", с замечаниями — %d", p.WarningsCount)
	}
	return text + "." + i.readerPage(nil) + i.projectPage()
}

func failedText(i *projectInfo) string {
	p := i.project
	reason := strings.TrimSpace(p.LastError)
	if reason == "" {
		reason = "ошибка провайдера"
	}
	if r := []rune(reason); len(r) > 300 {
		reason = string(r[:300]) + "…"
	}
	return fmt.Sprintf("Перевод %s на %s остановлен: %s.\nПродолжить можно на сайте или командой /status.",
		"<b>"+html.EscapeString(telegram.Clip(i.title, titleLimit))+"</b>", html.EscapeString(telegram.LanguageName(p.TargetLang)),
		html.EscapeString(strings.TrimSuffix(reason, "."))) + i.projectPage()
}
