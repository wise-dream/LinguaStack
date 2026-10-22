package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
)

// Смена модели посреди перевода (дополнение §5): переведённое остаётся,
// дальше переводит новая модель.

func changeModel(c *apiClient, projectID, configID, modelID string) *response {
	return c.do(http.MethodPatch, "/api/translations/"+projectID,
		map[string]any{"provider_config_id": configID, "model_id": modelID})
}

func TestChangeTranslationModel(t *testing.T) {
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nOne.\n\nTwo."))
	require.Equal(t, "fake-1", p.ModelID)
	other := e.ollamaConfig(t)

	r := changeModel(e.c, p.ID, other, " qwen3:8b ")
	require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
	var got projectBody
	r.JSON(t, &got)
	require.Equal(t, "qwen3:8b", got.ModelID)
	require.Equal(t, other, got.ProviderConfigID)
	require.Equal(t, "qwen3:8b", e.get(t, p.ID).ModelID)

	e.runWorker(t)
	for _, m := range e.fake.Models() {
		require.Equal(t, "qwen3:8b", m)
	}
	require.Equal(t, "completed", e.get(t, p.ID).Status)

	// Завершённому переводу модель тоже меняется: ею пойдут повторы
	// фрагментов.
	require.Equal(t, http.StatusOK, changeModel(e.c, p.ID, other, "qwen2.5-coder:14b").Status)
}

func TestChangeTranslationModelRules(t *testing.T) {
	e := newTranslationEnv(t, nil)
	p := e.project(t, e.book(t, "# Ch\n\nOne."))
	own := e.get(t, p.ID).ProviderConfigID

	r := changeModel(e.c, p.ID, own, "  ")
	require.Equal(t, http.StatusUnprocessableEntity, r.Status, "без модели")

	reader := e.signedIn(t, "reader@example.com")
	_, foreign := createConfig(t, reader, map[string]any{"provider_id": "ollama"})
	require.Equal(t, http.StatusUnprocessableEntity, changeModel(e.c, p.ID, foreign.ID, "fake-2").Status,
		"чужое подключение")
	require.Equal(t, http.StatusNotFound, changeModel(reader, p.ID, foreign.ID, "fake-2").Status,
		"чужой перевод из общей библиотеки — только читать")

	_, forbidden := createConfig(t, e.c, map[string]any{"provider_id": "anthropic", "api_key": secretKey, "allow_external": false})
	r = changeModel(e.c, p.ID, forbidden.ID, "claude-haiku-5-5")
	require.Equal(t, http.StatusBadRequest, r.Status, "передача наружу запрещена (ТЗ §6.4)")

	require.NoError(t, e.db.Model(&models.TranslationProject{}).Where("id = ?", p.ID).Update("status", models.ProjectRunning).Error)
	require.Equal(t, http.StatusOK, changeModel(e.c, p.ID, own, "fake-2").Status, "на ходу — подхватит следующий батч")

	require.NoError(t, e.db.Model(&models.TranslationProject{}).Where("id = ?", p.ID).Update("status", models.ProjectCancelled).Error)
	require.Equal(t, http.StatusConflict, changeModel(e.c, p.ID, own, "fake-3").Status, "отменённый не продолжить")
	require.Equal(t, "fake-2", e.get(t, p.ID).ModelID)
}
