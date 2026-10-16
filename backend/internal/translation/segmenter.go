// Package translation — ядро перевода: сегментация документа, протокол
// общения с моделью, проверка результата и перевод батча. Не зависит ни
// от формата книги, ни от конкретного провайдера (ТЗ §4.2, §6.1).
package translation

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/vnovels/linguastack/internal/cdm"
)

// SegmentRef — единица перевода. Ровно один переводимый блок CDM
// (дополнение §2): тогда проверка «сколько блоков на входе — столько на
// выходе» тривиальна, а повтор и ручная правка (ТЗ §12) адресуют блок.
type SegmentRef struct {
	NodeID       string
	ChapterIndex int
	Ordinal      int // позиция блока в главе
	Type         cdm.BlockType
	Text         string
	SourceHash   string
	Level        int
}

// Batch — сегменты одного запроса к модели и соседний текст для связности.
type Batch struct {
	Segments      []SegmentRef
	ContextBefore string
	ContextAfter  string
}

// SegmentTooLargeError — один блок больше лимита батча. Явная ошибка с
// именем блока вместо молчаливого обрезания (Review Focus №3): такой блок
// иначе никогда не был бы переведён целиком.
type SegmentTooLargeError struct {
	NodeID string
	Chars  int
	Limit  int
}

func (e *SegmentTooLargeError) Error() string {
	return fmt.Sprintf("блок %s длиннее лимита батча: %d символов при лимите %d", e.NodeID, e.Chars, e.Limit)
}

// contextChars — сколько символов соседнего текста передаётся для связности.
const contextChars = 600

// Segment выделяет переводимые непустые блоки в порядке чтения.
func Segment(doc cdm.Document) []SegmentRef {
	var out []SegmentRef
	var walk func([]cdm.Chapter)
	walk = func(chapters []cdm.Chapter) {
		for _, ch := range chapters {
			for ordinal, b := range ch.Blocks {
				if !cdm.IsTranslatable(b.Type) || strings.TrimSpace(b.Text) == "" {
					continue
				}
				out = append(out, SegmentRef{
					NodeID: b.NodeID, ChapterIndex: ch.Index, Ordinal: ordinal, Type: b.Type,
					Text: b.Text, SourceHash: b.SourceHash, Level: b.Level,
				})
			}
			walk(ch.Children)
		}
	}
	walk(doc.Chapters)
	return out
}

// BuildBatches группирует последовательные сегменты одной главы в батчи
// не больше maxSegments сегментов и maxChars символов. Длина — в символах,
// а не в байтах: кириллица занимает два байта, и лимит в байтах вдвое
// урезал бы русские батчи.
//
// Блоки длиннее maxChars в батчи не попадают и возвращаются отдельно:
// один слишком длинный абзац не должен останавливать перевод всей книги.
func BuildBatches(segs []SegmentRef, maxSegments, maxChars int) (batches []Batch, oversized []*SegmentTooLargeError) {
	if maxSegments < 1 {
		maxSegments = 1
	}
	var current []int // индексы сегментов текущего батча
	chars := 0
	flush := func() {
		if len(current) == 0 {
			return
		}
		batch := Batch{Segments: make([]SegmentRef, len(current))}
		for i, idx := range current {
			batch.Segments[i] = segs[idx]
		}
		first, last := current[0], current[len(current)-1]
		if first > 0 && segs[first-1].ChapterIndex == segs[first].ChapterIndex {
			batch.ContextBefore = tail(segs[first-1].Text, contextChars)
		}
		if last+1 < len(segs) && segs[last+1].ChapterIndex == segs[last].ChapterIndex {
			batch.ContextAfter = head(segs[last+1].Text, contextChars)
		}
		batches = append(batches, batch)
		current, chars = nil, 0
	}

	for i, s := range segs {
		n := utf8.RuneCountInString(s.Text)
		if maxChars > 0 && n > maxChars {
			oversized = append(oversized, &SegmentTooLargeError{NodeID: s.NodeID, Chars: n, Limit: maxChars})
			continue
		}
		sameChapter := len(current) == 0 || segs[current[0]].ChapterIndex == s.ChapterIndex
		fits := len(current) < maxSegments && (maxChars <= 0 || chars+n <= maxChars)
		if !sameChapter || !fits {
			flush()
		}
		current = append(current, i)
		chars += n
	}
	flush()
	return batches, oversized
}

func head(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

func tail(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}
