package booksearch

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/db/models"
)

// EnsureIndex готовит блоки оригинала к поиску: при первом поиске по книге
// записывает их в book_blocks, а если язык с тех пор стал известен иначе —
// переразбирает с новым словарём (tsvector пересчитывает сама база).
// Оригинал после импорта не меняется, поэтому текст не обновляется.
func EnsureIndex(ctx context.Context, db *gorm.DB, book *models.Book, lang string) error {
	db = db.WithContext(ctx)
	var indexed bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM book_blocks WHERE book_id = ?)`, book.ID).Scan(&indexed).Error; err != nil {
		return fmt.Errorf("блоки книги для поиска: %w", err)
	}
	if indexed {
		err := db.Exec(`UPDATE book_blocks SET search_config = ls_search_config(?)
			WHERE book_id = ? AND search_config <> ls_search_config(?)`, lang, book.ID, lang).Error
		if err != nil {
			return fmt.Errorf("язык блоков книги: %w", err)
		}
		return nil
	}

	type blockRow struct {
		N string `json:"n"`
		C int    `json:"c"`
		O int    `json:"o"`
		T string `json:"t"`
	}
	var blocks []blockRow
	walk(book.CDM.Data.Chapters, func(ch *cdm.Chapter) {
		for i, b := range ch.Blocks {
			if strings.TrimSpace(b.Text) != "" {
				blocks = append(blocks, blockRow{N: b.NodeID, C: ch.Index, O: i, T: b.Text})
			}
		}
	})
	if len(blocks) == 0 {
		return nil
	}
	// Одним запросом: в большой книге десятки тысяч блоков. Строки — одним
	// параметром jsonb: срез GORM развернул бы в список параметров. Два
	// поиска сразу — второй ничего не вставит.
	payload, err := json.Marshal(blocks)
	if err != nil {
		return fmt.Errorf("блоки книги для поиска: %w", err)
	}
	err = db.Exec(`INSERT INTO book_blocks (book_id, node_id, chapter_index, ordinal, text, search_config)
		SELECT ?, n, c, o, t, ls_search_config(?)
		  FROM jsonb_to_recordset(?::jsonb) AS u(n text, c int, o int, t text)
		ON CONFLICT DO NOTHING`, book.ID, lang, string(payload)).Error
	if err != nil {
		return fmt.Errorf("блоки книги для поиска: %w", err)
	}
	return nil
}

func walk(chapters []cdm.Chapter, visit func(*cdm.Chapter)) {
	for i := range chapters {
		visit(&chapters[i])
		walk(chapters[i].Children, visit)
	}
}
