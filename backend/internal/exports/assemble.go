// Package exports — задачи экспорта (ТЗ §11 ExportJob, §12): сборка
// документа из книги и перевода и выполнение задач worker-ом.
package exports

import (
	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/exporters"
)

// softBreakFormats — форматы, где «\n» внутри блока — мягкий перенос
// исходника Markdown, а не <br> (дополнение §16).
var softBreakFormats = map[string]bool{"markdown": true, "zip_repo": true}

// Assemble собирает документ для экспорта: блоки CDM в порядке оглавления
// книги (с перестановкой глав пользователем) и перевод каждого блока по
// node_id. Блок без перевода — пустого, упавшего или ещё не готового —
// выводится оригиналом и считается в untranslated (дополнение §16).
func Assemble(doc cdm.Document, sourceFormat string, segs []models.TranslationSegment, mode exporters.Mode,
	sourceLang, targetLang string) (*exporters.Book, int) {
	translated := make(map[string]string, len(segs))
	for _, s := range segs {
		if s.TranslatedText != nil && *s.TranslatedText != "" {
			translated[s.NodeID] = *s.TranslatedText
		}
	}
	untranslated := 0
	block := func(b cdm.Block) exporters.Block {
		out := exporters.Block{Type: b.Type, Level: b.Level, Meta: b.Meta, Original: b.Text}
		if cdm.IsTranslatable(b.Type) {
			out.Translation, out.Translated = translated[b.NodeID]
			if !out.Translated {
				untranslated++
			}
		}
		return out
	}
	var chapters func([]cdm.Chapter) []exporters.Chapter
	chapters = func(src []cdm.Chapter) []exporters.Chapter {
		if len(src) == 0 {
			return nil
		}
		out := make([]exporters.Chapter, len(src))
		for i, ch := range src {
			c := exporters.Chapter{Index: ch.Index, Title: ch.Title, Children: chapters(ch.Children)}
			for _, b := range ch.Blocks {
				c.Blocks = append(c.Blocks, block(b))
			}
			c.Title = translatedTitle(ch.Title, c.Blocks)
			out[i] = c
		}
		return out
	}

	book := &exporters.Book{
		Title: doc.Meta.Title, Author: doc.Meta.Author, Lang: targetLang, SourceLang: sourceLang,
		Mode: mode, HardBreaks: !softBreakFormats[sourceFormat], Chapters: chapters(doc.Chapters),
	}
	if len(book.Chapters) > 0 {
		book.Title = translatedTitle(book.Title, book.Chapters[0].Blocks)
	}
	return book, untranslated
}

// translatedTitle — перевод названия, если первый блок — заголовок с тем
// же текстом; иначе название как было (оно из оглавления EPUB или имени
// файла, и перевода у него нет).
func translatedTitle(title string, blocks []exporters.Block) string {
	if len(blocks) == 0 || blocks[0].Type != cdm.Heading || !blocks[0].Translated {
		return title
	}
	if cdm.NormalizeText(exporters.PlainText(blocks[0].Original)) != cdm.NormalizeText(title) {
		return title
	}
	if t := exporters.PlainText(blocks[0].Translation); t != "" {
		return t
	}
	return title
}
