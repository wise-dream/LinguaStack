package booksearch

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/db/models"
)

const (
	snippetRadius = 60
	// Метки найденного в ts_headline — из области частного использования
	// Юникода: в тексте книги их не бывает.
	markOpen  = "\ue000"
	markClose = "\ue001"
)

// Стороны книги, в которых найдено совпадение.
const (
	InOriginal    = "original"
	InTranslation = "translation"
)

// Hit — найденный блок: отрывок вокруг совпадения и сами найденные слова
// (в том виде, как они стоят в тексте) — для подсветки.
type Hit struct {
	ChapterIndex int      `json:"chapter_index"`
	NodeID       string   `json:"node_id"`
	In           string   `json:"in"`
	Snippet      string   `json:"snippet"`
	Hits         []string `json:"hits"`

	ordinal int
}

// Result — первые Limit совпадений в порядке чтения и число всех.
type Result struct {
	Results   []Hit `json:"results"`
	Total     int   `json:"total"`
	Truncated bool  `json:"truncated"`
	// Suggestion — запрос с исправленной опечаткой, если ничего не нашлось.
	Suggestion string `json:"suggestion,omitempty"`
}

// Params — где и что искать. Project — выбранный перевод этой книги
// (владелец проверен вызывающим) или nil: тогда только оригинал.
type Params struct {
	Book    *models.Book
	Project *models.TranslationProject
	Query   string
	Limit   int
}

// side — одна из сторон поиска: таблица, текст, язык и условие отбора.
type side struct {
	in     string
	lang   string
	from   string // таблица с search_tsv и text-колонкой
	text   string // выражение текста
	filter string // условие отбора строк книги или перевода
	arg    any
}

// Search ищет по словам, если по словам ничего нет — подстрокой, если и
// так ничего — подсказывает исправленный запрос.
func Search(ctx context.Context, db *gorm.DB, p Params) (Result, error) {
	db = db.WithContext(ctx)
	q := Parse(p.Query)
	lang, err := originalLanguage(db, p)
	if err != nil {
		return Result{}, err
	}
	if err := EnsureIndex(ctx, db, p.Book, lang); err != nil {
		return Result{}, err
	}
	sides := []side{{in: InOriginal, lang: lang, from: "book_blocks", text: "text", filter: "book_id = ?", arg: p.Book.ID}}
	if p.Project != nil {
		sides = append(sides, side{in: InTranslation, lang: p.Project.TargetLang, from: "translation_segments",
			text: "translated_text", filter: "project_id = ? AND translated_text IS NOT NULL", arg: p.Project.ID})
	}

	var hits []Hit
	words := !q.CJK && len(q.Phrases)+len(q.Words) > 0
	if words {
		if hits, err = byWords(db, sides, q); err != nil {
			return Result{}, err
		}
	}
	bySubstring := len(hits) == 0
	if bySubstring {
		if hits, err = substring(db, sides, q.Raw); err != nil {
			return Result{}, err
		}
	}

	order := readingOrder(&p.Book.CDM.Data)
	slices.SortStableFunc(hits, func(a, b Hit) int {
		if d := order[a.ChapterIndex] - order[b.ChapterIndex]; d != 0 {
			return d
		}
		if a.ordinal != b.ordinal {
			return a.ordinal - b.ordinal
		}
		return strings.Compare(a.In, b.In) // оригинал раньше перевода
	})
	res := Result{Total: len(hits), Truncated: len(hits) > p.Limit}
	hits = hits[:min(len(hits), p.Limit)]
	if !bySubstring {
		if err := headlines(db, sides, q, hits); err != nil {
			return Result{}, err
		}
	}
	res.Results = hits
	if res.Results == nil {
		res.Results = []Hit{}
	}
	if res.Total == 0 && words {
		if res.Suggestion, err = suggest(db, sides, q); err != nil {
			return Result{}, err
		}
	}
	return res, nil
}

// originalLanguage — язык оригинала: метаданные книги, язык оригинала у
// выбранного перевода, затем у остальных переводов книги, затем алфавит.
func originalLanguage(db *gorm.DB, p Params) (string, error) {
	var langs []string
	if p.Project != nil {
		langs = append(langs, p.Project.SourceLang)
	}
	var others []string
	err := db.Model(&models.TranslationProject{}).Where("book_id = ?", p.Book.ID).
		Order("created_at DESC").Pluck("source_lang", &others).Error
	if err != nil {
		return "", fmt.Errorf("языки переводов книги: %w", err)
	}
	return OriginalLanguage(p.Book.Language, append(langs, others...), &p.Book.CDM.Data), nil
}

// tsquery — запрос в синтаксисе полнотекстового поиска: все фразы и все
// слова (&&), последнее недописанное слово — как начало слова. Слова — только
// буквы и цифры (Parse), поэтому «слово:*» в to_tsquery безопасно. Слово из
// одних стоп-слов даёт пустой запрос — numnode отсекает его в byWords.
func tsquery(q Query, lang string) (string, []any) {
	var parts []string
	var args []any
	for _, ph := range q.Phrases {
		parts = append(parts, "phraseto_tsquery(ls_search_config(?), ?)")
		args = append(args, lang, ph)
	}
	for i, w := range q.Words {
		if q.Prefix && i == len(q.Words)-1 {
			parts = append(parts, "to_tsquery(ls_search_config(?), ?)")
			args = append(args, lang, w+":*")
			continue
		}
		parts = append(parts, "plainto_tsquery(ls_search_config(?), ?)")
		args = append(args, lang, w)
	}
	return "(" + strings.Join(parts, " && ") + ")", args
}

func byWords(db *gorm.DB, sides []side, q Query) ([]Hit, error) {
	var hits []Hit
	for _, s := range sides {
		expr, args := tsquery(q, s.lang)
		var rows []struct {
			NodeID       string
			ChapterIndex int
			Ordinal      int
		}
		sql := fmt.Sprintf(`SELECT node_id, chapter_index, ordinal FROM %s
			WHERE %s AND numnode(%s) > 0 AND search_tsv @@ %s`, s.from, s.filter, expr, expr)
		all := append(append([]any{s.arg}, args...), args...)
		if err := db.Raw(sql, all...).Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("поиск по словам: %w", err)
		}
		for _, r := range rows {
			hits = append(hits, Hit{ChapterIndex: r.ChapterIndex, NodeID: r.NodeID, In: s.in, ordinal: r.Ordinal})
		}
	}
	return hits, nil
}

// headlines заполняет отрывки показанных результатов: ts_headline выбирает
// кусок с найденными словами и отмечает их.
func headlines(db *gorm.DB, sides []side, q Query, hits []Hit) error {
	// Короткий блок — целиком со всеми отметками; из длинного — один кусок
	// вокруг найденного (в режиме кусков ts_headline обрезает и короткий).
	const marks = "StartSel=" + markOpen + ", StopSel=" + markClose
	const whole, fragment = marks + ", HighlightAll=true", marks + ", MaxWords=30, MinWords=12, MaxFragments=1"
	for _, s := range sides {
		var nodes []string
		for _, h := range hits {
			if h.In == s.in {
				nodes = append(nodes, h.NodeID)
			}
		}
		if len(nodes) == 0 {
			continue
		}
		expr, args := tsquery(q, s.lang)
		var rows []struct {
			NodeID   string
			Text     string
			Headline string
		}
		sql := fmt.Sprintf(`SELECT node_id, %s AS text, ts_headline(ls_search_config(?), %s, %s,
				CASE WHEN char_length(%s) <= ? THEN ? ELSE ? END) AS headline
			FROM %s WHERE %s AND node_id IN ?`, s.text, s.text, expr, s.text, s.from, s.filter)
		all := append(append([]any{s.lang}, args...), 2*snippetRadius, whole, fragment, s.arg, nodes)
		if err := db.Raw(sql, all...).Scan(&rows).Error; err != nil {
			return fmt.Errorf("отрывки найденного: %w", err)
		}
		byNode := make(map[string]int, len(rows))
		for i, r := range rows {
			byNode[r.NodeID] = i
		}
		for i := range hits {
			if hits[i].In != s.in {
				continue
			}
			if j, ok := byNode[hits[i].NodeID]; ok {
				hits[i].Snippet, hits[i].Hits = unmark(rows[j].Headline, rows[j].Text)
			}
		}
	}
	return nil
}

// unmark снимает метки ts_headline: возвращает отрывок с многоточиями там,
// где он обрезан, и отмеченные слова без повторов.
func unmark(headline, text string) (string, []string) {
	var words []string
	var plain strings.Builder
	rest := headline
	for {
		open := strings.Index(rest, markOpen)
		if open < 0 {
			plain.WriteString(rest)
			break
		}
		plain.WriteString(rest[:open])
		rest = rest[open+len(markOpen):]
		end := strings.Index(rest, markClose)
		if end < 0 {
			plain.WriteString(rest)
			break
		}
		word := rest[:end]
		plain.WriteString(word)
		if !slices.Contains(words, word) {
			words = append(words, word)
		}
		rest = rest[end+len(markClose):]
	}
	core := strings.TrimSpace(plain.String())
	snippet := core
	if at := strings.Index(text, core); at >= 0 {
		if strings.TrimSpace(text[:at]) != "" {
			snippet = "…" + snippet
		}
		if strings.TrimSpace(text[at+len(core):]) != "" {
			snippet += "…"
		}
	}
	return snippet, words
}

// substring — запасной поиск: запрос целиком как подстрока без учёта
// регистра. Находит кусок слова или кода и текст без пробелов между словами.
func substring(db *gorm.DB, sides []side, raw string) ([]Hit, error) {
	needle := foldRunes(raw)
	var hits []Hit
	for _, s := range sides {
		var rows []struct {
			NodeID       string
			ChapterIndex int
			Ordinal      int
			Text         string
		}
		sql := fmt.Sprintf(`SELECT node_id, chapter_index, ordinal, %s AS text FROM %s
			WHERE %s AND %s ILIKE ? ESCAPE '\'`, s.text, s.from, s.filter, s.text)
		if err := db.Raw(sql, s.arg, "%"+escapeLike(raw)+"%").Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("поиск подстроки: %w", err)
		}
		for _, r := range rows {
			snippet, found, ok := matchSnippet(r.Text, needle)
			if !ok {
				// ILIKE и посимвольная свёртка регистра разошлись (редкие
				// буквы): блок найден, отрывок — его начало.
				snippet, found = cut(r.Text), raw
			}
			hits = append(hits, Hit{ChapterIndex: r.ChapterIndex, NodeID: r.NodeID, In: s.in,
				Snippet: snippet, Hits: []string{found}, ordinal: r.Ordinal})
		}
	}
	return hits, nil
}

// suggest — запрос, где каждое слово, которого нет в книге, заменено самым
// похожим словом книги (сходство триграмм pg_trgm). "" — заменить нечего.
// Слова книги собирает ts_stat: строчными, как они стоят в тексте.
func suggest(db *gorm.DB, sides []side, q Query) (string, error) {
	if len(q.Words) == 0 {
		return "", nil
	}
	var sources []string
	for _, s := range sides {
		// ts_stat принимает текст запроса: идентификатор — UUID из базы,
		// подстановка безопасна.
		sources = append(sources, fmt.Sprintf("SELECT to_tsvector('simple', %s) FROM %s WHERE %s",
			s.text, s.from, strings.Replace(s.filter, "?", "'"+fmt.Sprint(s.arg)+"'", 1)))
	}
	words, err := json.Marshal(q.Words)
	if err != nil {
		return "", err
	}
	var rows []struct {
		Word string
		Best *string
	}
	err = db.Raw(`WITH book_words AS MATERIALIZED (SELECT word, nentry FROM ts_stat(?))
		SELECT q.word, (SELECT w.word FROM book_words w WHERE w.word % q.word
		                 ORDER BY similarity(w.word, q.word) DESC, w.nentry DESC, w.word LIMIT 1) AS best
		  FROM jsonb_array_elements_text(?::jsonb) WITH ORDINALITY AS q(word, i) ORDER BY q.i`,
		strings.Join(sources, " UNION ALL "), string(words)).Scan(&rows).Error
	if err != nil {
		return "", fmt.Errorf("подсказка: %w", err)
	}
	changed := false
	parts := make([]string, 0, len(q.Phrases)+len(rows))
	for _, ph := range q.Phrases {
		parts = append(parts, `"`+ph+`"`)
	}
	for _, r := range rows {
		if r.Best != nil && *r.Best != r.Word {
			parts = append(parts, *r.Best)
			changed = true
			continue
		}
		parts = append(parts, r.Word)
	}
	if !changed {
		return "", nil
	}
	return strings.Join(parts, " "), nil
}

func readingOrder(doc *cdm.Document) map[int]int {
	order := map[int]int{}
	walk(doc.Chapters, func(ch *cdm.Chapter) { order[ch.Index] = len(order) })
	return order
}

// foldRunes — строчные буквы по одной на символ: так позиция совпадения в
// свёрнутом тексте совпадает с позицией в исходном (strings.ToLower может
// менять длину).
func foldRunes(s string) []rune {
	out := []rune(s)
	for i, r := range out {
		out[i] = unicode.ToLower(r)
	}
	return out
}

// matchSnippet ищет needle без учёта регистра и возвращает отрывок вокруг
// первого совпадения и само совпадение, как оно написано в тексте.
func matchSnippet(text string, needle []rune) (snippet, found string, ok bool) {
	runes := []rune(text)
	folded := foldRunes(text)
	at := -1
	for i := 0; i+len(needle) <= len(folded); i++ {
		if slices.Equal(folded[i:i+len(needle)], needle) {
			at = i
			break
		}
	}
	if at < 0 {
		return "", "", false
	}
	start, end := max(0, at-snippetRadius), min(len(runes), at+len(needle)+snippetRadius)
	snippet = string(runes[start:end])
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(runes) {
		snippet += "…"
	}
	return snippet, string(runes[at : at+len(needle)]), true
}

func cut(text string) string {
	runes := []rune(text)
	if len(runes) <= 2*snippetRadius {
		return text
	}
	return string(runes[:2*snippetRadius]) + "…"
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func escapeLike(s string) string { return likeEscaper.Replace(s) }
