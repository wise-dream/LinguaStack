package booksearch

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/testdb"
)

func TestEnsureIndex(t *testing.T) {
	db := testdb.New(t)
	user := testdb.MakeUser(t, db)
	book := testdb.MakeBook(t, db, user.ID, &cdm.Document{Chapters: []cdm.Chapter{
		{Index: 0, Blocks: []cdm.Block{{NodeID: "a", Text: "Cats slept."}, {NodeID: "empty", Text: "  "}}},
		{Index: 1, Children: []cdm.Chapter{{Index: 2, Blocks: []cdm.Block{{NodeID: "b", Text: "Dogs ran."}}}}},
	}})
	type row struct {
		NodeID       string
		ChapterIndex int
		Ordinal      int
		Config       string
	}
	rows := func() []row {
		var out []row
		require.NoError(t, db.Raw(`SELECT node_id, chapter_index, ordinal, search_config::text AS config
			FROM book_blocks WHERE book_id = ? ORDER BY node_id`, book.ID).Scan(&out).Error)
		return out
	}

	require.NoError(t, EnsureIndex(t.Context(), db, book, "en"))
	require.Equal(t, []row{{"a", 0, 0, "english"}, {"b", 2, 0, "english"}}, rows(), "вложенные главы — тоже; пустых блоков нет")

	// Язык стал известен иначе — блоки переразбираются, а не дублируются.
	require.NoError(t, EnsureIndex(t.Context(), db, book, "de"))
	require.Equal(t, []row{{"a", 0, 0, "german"}, {"b", 2, 0, "german"}}, rows())

	require.NoError(t, EnsureIndex(t.Context(), db, book, ""))
	require.Equal(t, "simple", rows()[0].Config, "неизвестный язык — без словаря форм")
}
