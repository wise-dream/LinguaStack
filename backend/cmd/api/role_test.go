package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/testdb"
)

func TestSetRoleCLI(t *testing.T) {
	tx := testdb.New(t)
	user := testdb.MakeUser(t, tx)
	role := func() string {
		var u models.User
		require.NoError(t, tx.First(&u, "id = ?", user.ID).Error)
		return u.Role
	}

	require.NoError(t, setRole(t.Context(), tx, "  "+user.Email+"=admin"))
	require.Equal(t, models.RoleAdmin, role())
	require.NoError(t, setRole(t.Context(), tx, user.Email+"=user"))
	require.Equal(t, models.RoleUser, role())

	for _, bad := range []string{"", "admin", user.Email + "=root", "=admin", "nobody@example.com=admin"} {
		require.Error(t, setRole(t.Context(), tx, bad), "%q", bad)
	}
	require.Equal(t, models.RoleUser, role())
}
