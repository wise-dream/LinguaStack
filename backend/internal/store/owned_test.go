package store_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/store"
	"github.com/vnovels/linguastack/internal/testdb"
)

// Модели ресурсов появятся в следующих задачах, поэтому тест заводит свою
// таблицу внутри транзакции — откат уберёт и её (DDL в Postgres транзакционен).
type ownedFixture struct {
	models.Base
	OwnerID uuid.UUID `gorm:"type:uuid"`
	Title   string
}

func (ownedFixture) TableName() string { return "owned_fixtures" }

type unownedFixture struct {
	models.Base
	Title string
}

func (unownedFixture) TableName() string { return "owned_fixtures" }

func setup(t *testing.T) *gorm.DB {
	t.Helper()
	tx := testdb.New(t)
	require.NoError(t, tx.Exec(`CREATE TEMP TABLE owned_fixtures (
		id uuid PRIMARY KEY, owner_id uuid NOT NULL, title text NOT NULL,
		created_at timestamptz NOT NULL DEFAULT now())`).Error)
	return tx
}

func makeUser(t *testing.T, tx *gorm.DB, email, role string) models.User {
	t.Helper()
	u := models.User{Email: email, PasswordHash: "x", Role: role, IsActive: true}
	require.NoError(t, tx.Create(&u).Error)
	return u
}

func TestOwnedReturnsResourceToOwner(t *testing.T) {
	tx := setup(t)
	owner := makeUser(t, tx, "owner@example.com", models.RoleUser)
	row := ownedFixture{OwnerID: owner.ID, Title: "моя книга"}
	require.NoError(t, tx.Create(&row).Error)

	got, err := store.Owned[ownedFixture](t.Context(), tx, row.ID, owner.ID)
	require.NoError(t, err)
	require.Equal(t, "моя книга", got.Title)
}

func TestOwnedReturnsNotFoundForForeignResource(t *testing.T) {
	tx := setup(t)
	owner := makeUser(t, tx, "owner@example.com", models.RoleUser)
	intruder := makeUser(t, tx, "intruder@example.com", models.RoleUser)
	row := ownedFixture{OwnerID: owner.ID, Title: "чужая"}
	require.NoError(t, tx.Create(&row).Error)

	_, err := store.Owned[ownedFixture](t.Context(), tx, row.ID, intruder.ID)
	// Не отдельная ошибка «запрещено»: она подтвердила бы существование ресурса.
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestAdminRoleDoesNotBypass(t *testing.T) {
	// ТЗ §2.1: доступ к содержимому книг не возникает из одной роли admin.
	// Owned принимает идентификатор владельца, а не пользователя, — роли
	// некуда попасть, обход исключён формой API.
	tx := setup(t)
	owner := makeUser(t, tx, "owner@example.com", models.RoleUser)
	admin := makeUser(t, tx, "admin@example.com", models.RoleAdmin)
	row := ownedFixture{OwnerID: owner.ID, Title: "личное"}
	require.NoError(t, tx.Create(&row).Error)

	_, err := store.Owned[ownedFixture](t.Context(), tx, row.ID, admin.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestOwnedNotFoundForMissingID(t *testing.T) {
	tx := setup(t)
	owner := makeUser(t, tx, "owner@example.com", models.RoleUser)
	_, err := store.Owned[ownedFixture](t.Context(), tx, uuid.New(), owner.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestOwnedRejectsModelWithoutOwnerColumn(t *testing.T) {
	// Модель без owner_id — ошибка конфигурации. Она не должна выглядеть
	// ни как «найдено», ни как «не найдено», иначе её замаскирует 404.
	tx := setup(t)
	_, err := store.Owned[unownedFixture](t.Context(), tx, uuid.New(), uuid.New())
	require.ErrorIs(t, err, store.ErrNoOwnerColumn)
	require.False(t, errors.Is(err, store.ErrNotFound))
}

func TestOwnedScopeFiltersLists(t *testing.T) {
	tx := setup(t)
	owner := makeUser(t, tx, "owner@example.com", models.RoleUser)
	other := makeUser(t, tx, "other@example.com", models.RoleUser)
	for _, r := range []ownedFixture{
		{OwnerID: owner.ID, Title: "a"}, {OwnerID: owner.ID, Title: "b"}, {OwnerID: other.ID, Title: "c"},
	} {
		require.NoError(t, tx.Create(&r).Error)
	}

	var mine []ownedFixture
	require.NoError(t, tx.Scopes(store.OwnedScope(owner.ID)).Order("title").Find(&mine).Error)
	require.Len(t, mine, 2)
	require.Equal(t, "a", mine[0].Title)
	require.Equal(t, "b", mine[1].Title)
}
