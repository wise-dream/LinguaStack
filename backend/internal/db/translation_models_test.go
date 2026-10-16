package db_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/testdb"
)

func segment(project *models.TranslationProject, nodeID, text string) models.TranslationSegment {
	return models.TranslationSegment{
		ProjectID: project.ID, NodeID: nodeID, BlockType: "paragraph",
		SourceText: text, SourceHash: "h", Status: models.SegmentPending, Severity: "ok",
	}
}

func TestSegmentNodeIDUniquePerProject(t *testing.T) {
	tx := testdb.New(t)
	project := testdb.MakeProject(t, tx, nil)
	first := segment(project, "ch0000/b0000", "x")
	require.NoError(t, tx.Create(&first).Error)
	dup := segment(project, "ch0000/b0000", "y")
	err := tx.Transaction(func(inner *gorm.DB) error { return inner.Create(&dup).Error })
	require.ErrorIs(t, err, gorm.ErrDuplicatedKey)
}

func TestTwoProjectsOnOneBookIndependent(t *testing.T) {
	// ТЗ §5.1 и Review Focus №4: en→ru и en→de на одной книге не мешают друг другу.
	tx := testdb.New(t)
	ru := testdb.MakeProject(t, tx, &testdb.ProjectOptions{TargetLang: "ru"})
	de := testdb.MakeProject(t, tx, &testdb.ProjectOptions{TargetLang: "de", Book: &ru.BookID, Owner: &ru.OwnerID})
	require.Equal(t, ru.BookID, de.BookID)

	for _, p := range []*models.TranslationProject{ru, de} {
		s := segment(p, "ch0000/b0000", "Book")
		require.NoError(t, tx.Create(&s).Error, "один node_id в двух проектах — законно")
	}
	var count int64
	require.NoError(t, tx.Model(&models.TranslationSegment{}).Where("project_id = ?", ru.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestRevisionHistoryPreserved(t *testing.T) {
	// ТЗ §5.1: повторный перевод не уничтожает предыдущую версию.
	tx := testdb.New(t)
	project := testdb.MakeProject(t, tx, nil)
	s := segment(project, "ch0000/b0000", "Book")
	require.NoError(t, tx.Create(&s).Error)
	for _, rev := range []models.TranslationSegmentRevision{
		{SegmentID: s.ID, TranslatedText: "Первый вариант", Origin: models.OriginMachine},
		{SegmentID: s.ID, TranslatedText: "Второй вариант", Origin: models.OriginRetry},
		{SegmentID: s.ID, TranslatedText: "Мой вариант", Origin: models.OriginManual},
	} {
		require.NoError(t, tx.Create(&rev).Error)
	}
	var revisions []models.TranslationSegmentRevision
	require.NoError(t, tx.Where("segment_id = ?", s.ID).Order("created_at, id").Find(&revisions).Error)
	require.Len(t, revisions, 3)

	bad := models.TranslationSegmentRevision{SegmentID: s.ID, TranslatedText: "x", Origin: "magic"}
	require.Error(t, tx.Transaction(func(inner *gorm.DB) error { return inner.Create(&bad).Error }))
}

func TestStatusesConstrainedBySchema(t *testing.T) {
	// ТЗ §7: статусы задачи — ровно pending, running, completed, failed, cancelled.
	require.ElementsMatch(t, []string{"pending", "running", "completed", "failed", "cancelled"}, models.TaskStatuses)
	tx := testdb.New(t)
	project := testdb.MakeProject(t, tx, nil)

	task := models.TranslationTask{ProjectID: project.ID, OwnerID: project.OwnerID, Status: "paused", MaxAttempts: 3}
	require.Error(t, tx.Transaction(func(inner *gorm.DB) error { return inner.Create(&task).Error }),
		"пауза живёт на проекте, а не на задаче (ADD §7)")

	require.Error(t, tx.Transaction(func(inner *gorm.DB) error {
		return inner.Model(project).Update("status", "sleeping").Error
	}))
}

func TestOnlyOneActiveTaskPerProject(t *testing.T) {
	// Двойное «Перевести» не ставит книгу в очередь дважды (ТЗ §7).
	tx := testdb.New(t)
	project := testdb.MakeProject(t, tx, nil)
	first := models.TranslationTask{ProjectID: project.ID, OwnerID: project.OwnerID, Status: models.TaskPending, MaxAttempts: 3}
	require.NoError(t, tx.Create(&first).Error)
	second := models.TranslationTask{ProjectID: project.ID, OwnerID: project.OwnerID, Status: models.TaskPending, MaxAttempts: 3}
	err := tx.Transaction(func(inner *gorm.DB) error { return inner.Create(&second).Error })
	require.ErrorIs(t, err, gorm.ErrDuplicatedKey)

	// Завершённая задача не мешает новой.
	require.NoError(t, tx.Model(&first).Update("status", models.TaskCompleted).Error)
	require.NoError(t, tx.Create(&second).Error)
}

func TestProgressPercent(t *testing.T) {
	// Review Focus №1: 0 сегментов — это 100%, а не деление на ноль.
	require.InDelta(t, 100.0, (&models.TranslationProject{}).ProgressPercent(), 0)
	require.InDelta(t, 25.0, (&models.TranslationProject{SegmentsTotal: 4, SegmentsDone: 1}).ProgressPercent(), 1e-9)
	require.InDelta(t, 50.0, (&models.TranslationProject{SegmentsTotal: 4, SegmentsDone: 1, SegmentsFailed: 1}).ProgressPercent(), 1e-9,
		"сегмент, исчерпавший попытки, тоже обработан")
}

func TestTerminalStatuses(t *testing.T) {
	for status, terminal := range map[string]bool{
		models.ProjectDraft: false, models.ProjectQueued: false, models.ProjectRunning: false, models.ProjectPaused: false,
		models.ProjectCompleted: true, models.ProjectCompletedWithWarnings: true, models.ProjectFailed: true, models.ProjectCancelled: true,
	} {
		require.Equal(t, terminal, (&models.TranslationProject{Status: status}).IsTerminal(), status)
	}
}

func TestDeletingBookCascades(t *testing.T) {
	// ТЗ §13: удаление книги не оставляет висящих проектов, сегментов и задач.
	tx := testdb.New(t)
	project := testdb.MakeProject(t, tx, nil)
	s := segment(project, "ch0000/b0000", "x")
	require.NoError(t, tx.Create(&s).Error)
	require.NoError(t, tx.Create(&models.TranslationSegmentRevision{SegmentID: s.ID, TranslatedText: "y", Origin: models.OriginMachine}).Error)
	require.NoError(t, tx.Create(&models.TranslationTask{ProjectID: project.ID, OwnerID: project.OwnerID, Status: models.TaskPending, MaxAttempts: 3}).Error)

	require.NoError(t, tx.Delete(&models.Book{}, "id = ?", project.BookID).Error)
	// Подсчёт — только по своим строкам: тесты очереди в соседних пакетах
	// коммитят данные, и счёт по всей таблице стал бы случайным.
	counts := map[string]*gorm.DB{
		"проекты": tx.Model(&models.TranslationProject{}).Where("id = ?", project.ID),
		"сегменты": tx.Model(&models.TranslationSegment{}).Where("project_id = ?", project.ID),
		"ревизии":  tx.Model(&models.TranslationSegmentRevision{}).Where("segment_id = ?", s.ID),
		"задачи":   tx.Model(&models.TranslationTask{}).Where("project_id = ?", project.ID),
	}
	for name, q := range counts {
		var n int64
		require.NoError(t, q.Count(&n).Error)
		require.Zero(t, n, name)
	}
}

func TestDeletingUserCascadesThroughConfigsAndProjects(t *testing.T) {
	// Проверка внешнего ключа конфигурации не должна мешать каскаду.
	tx := testdb.New(t)
	project := testdb.MakeProject(t, tx, nil)
	require.NoError(t, tx.Delete(&models.User{}, "id = ?", project.OwnerID).Error)
	var n int64
	require.NoError(t, tx.Model(&models.ProviderConfig{}).Where("id = ?", project.ProviderConfigID).Count(&n).Error)
	require.Zero(t, n)
}

func TestProviderConfigInUseCannotBeDeleted(t *testing.T) {
	// Иначе проект остался бы без модели посреди книги.
	tx := testdb.New(t)
	project := testdb.MakeProject(t, tx, nil)
	err := tx.Transaction(func(inner *gorm.DB) error {
		return inner.Delete(&models.ProviderConfig{}, "id = ?", project.ProviderConfigID).Error
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, gorm.ErrForeignKeyViolated), "получено: %v", err)
}
