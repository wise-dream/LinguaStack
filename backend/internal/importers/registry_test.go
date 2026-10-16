package importers_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/importers"
)

func defaultRegistry(t *testing.T) *importers.Registry {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)
	return importers.Default(cfg)
}

func TestPickTXTByExtension(t *testing.T) {
	imp, err := defaultRegistry(t).Pick([]byte("text"), "a.txt")
	require.NoError(t, err)
	require.Equal(t, "txt", imp.FormatID())
}

func TestPickIsCaseInsensitive(t *testing.T) {
	imp, err := defaultRegistry(t).Pick([]byte("text"), "README.TXT")
	require.NoError(t, err)
	require.Equal(t, "txt", imp.FormatID())
}

func TestPickRejectsUnknown(t *testing.T) {
	for _, name := range []string{"a.bin", "a.exe", "noext", ""} {
		_, err := defaultRegistry(t).Pick([]byte("\x00\x01"), name)
		require.ErrorIs(t, err, importers.ErrUnsupportedFormat, "файл %q", name)
	}
}
