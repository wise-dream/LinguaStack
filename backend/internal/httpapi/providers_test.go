package httpapi_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/httpapi"
	"github.com/vnovels/linguastack/internal/providers"
	"github.com/vnovels/linguastack/internal/testdb"
)

const secretKey = "sk-ant-super-secret-value-9876"

type configBody struct {
	ID                  string  `json:"id"`
	ProviderID          string  `json:"provider_id"`
	Name                string  `json:"name"`
	BaseURL             string  `json:"base_url"`
	HasAPIKey           bool    `json:"has_api_key"`
	KeyFingerprint      *string `json:"key_fingerprint"`
	KeyLast4            *string `json:"key_last4"`
	AllowExternal       bool    `json:"allow_external"`
	SendsDataExternally bool    `json:"sends_data_externally"`
}

func createConfig(t *testing.T, c *apiClient, body map[string]any) (*response, configBody) {
	t.Helper()
	r := c.do(http.MethodPost, "/api/provider-configs", body)
	var out configBody
	if r.Status == http.StatusCreated {
		r.JSON(t, &out)
	}
	return r, out
}

func TestProvidersCatalogDeclaresExternalEgress(t *testing.T) {
	// ТЗ §6.3: интерфейс обязан явно сообщать, что текст покинет сервер.
	c := newTestEnv(t).signedIn(t, "u@example.com")
	var kinds []providers.Kind
	c.do(http.MethodGet, "/api/providers", nil).JSON(t, &kinds)
	byID := map[string]providers.Kind{}
	for _, k := range kinds {
		byID[k.ID] = k
	}
	require.False(t, byID["ollama"].SendsDataExternally)
	require.True(t, byID["anthropic"].SendsDataExternally)
	require.True(t, byID["openai_compatible"].SendsDataExternally)
}

func TestAPIKeyNeverReturned(t *testing.T) {
	// ТЗ §15.14, §6.3: ключ не раскрывается клиенту после сохранения.
	c := newTestEnv(t).signedIn(t, "u@example.com")
	r, created := createConfig(t, c, map[string]any{"provider_id": "anthropic", "api_key": secretKey, "allow_external": true})
	require.Equal(t, http.StatusCreated, r.Status, "тело: %s", r.Body)
	require.NotContains(t, string(r.Body), secretKey)
	require.True(t, created.HasAPIKey)
	require.Equal(t, "9876", *created.KeyLast4)
	require.Len(t, *created.KeyFingerprint, 8)
	require.True(t, created.SendsDataExternally)

	for _, path := range []string{"/api/provider-configs", "/api/provider-configs/" + created.ID} {
		require.NotContains(t, string(c.do(http.MethodGet, path, nil).Body), secretKey, path)
	}
}

func TestAPIKeyNeverLogged(t *testing.T) {
	// Сырой (без фильтра) обработчик: проверяется, что код вообще не
	// пишет ключ в лог, а не то, что фильтр успел его вырезать.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	c := newTestEnv(t).signedIn(t, "u@example.com")
	_, created := createConfig(t, c, map[string]any{"provider_id": "anthropic", "api_key": secretKey})
	c.do(http.MethodGet, "/api/provider-configs/"+created.ID, nil)
	c.do(http.MethodDelete, "/api/provider-configs/"+created.ID, nil)
	require.NotContains(t, buf.String(), secretKey)
}

func TestCiphertextInDBIsNotPlaintext(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "u@example.com")
	_, created := createConfig(t, c, map[string]any{"provider_id": "anthropic", "api_key": secretKey})

	var row models.ProviderConfig
	require.NoError(t, env.db.Where("id = ?", created.ID).First(&row).Error)
	require.NotEmpty(t, row.SecretCiphertext)
	require.NotContains(t, row.SecretCiphertext, secretKey)
}

func TestForeignProviderConfigIs404(t *testing.T) {
	env := newTestEnv(t)
	owner, intruder := env.signedIn(t, "owner@example.com"), env.signedIn(t, "intruder@example.com")
	_, created := createConfig(t, owner, map[string]any{"provider_id": "anthropic", "api_key": secretKey})
	base := "/api/provider-configs/" + created.ID
	for _, req := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, base, nil},
		{http.MethodPatch, base, map[string]any{"allow_external": true}},
		{http.MethodDelete, base, nil},
		{http.MethodGet, base + "/models", nil},
		{http.MethodGet, base + "/health", nil},
	} {
		require.Equal(t, http.StatusNotFound, intruder.do(req.method, req.path, req.body).Status, "%s %s", req.method, req.path)
	}
	require.Equal(t, http.StatusOK, owner.do(http.MethodGet, base, nil).Status)
}

func TestOllamaConfigNeedsNoKeyAndRejectsUserURL(t *testing.T) {
	c := newTestEnv(t).signedIn(t, "u@example.com")
	r, created := createConfig(t, c, map[string]any{"provider_id": "ollama"})
	require.Equal(t, http.StatusCreated, r.Status, "тело: %s", r.Body)
	require.Nil(t, created.KeyLast4)
	require.False(t, created.SendsDataExternally)

	// Адрес Ollama задаёт администратор: пользовательский открыл бы путь к SSRF.
	r, _ = createConfig(t, c, map[string]any{"provider_id": "ollama", "base_url": "http://169.254.169.254/"})
	require.Equal(t, http.StatusUnprocessableEntity, r.Status)
}

func TestConfigValidation(t *testing.T) {
	c := newTestEnv(t).signedIn(t, "u@example.com")
	cases := map[string]map[string]any{
		"неизвестный провайдер":       {"provider_id": "mystery"},
		"Anthropic без ключа":         {"provider_id": "anthropic"},
		"Anthropic со своим адресом":  {"provider_id": "anthropic", "api_key": secretKey, "base_url": "https://evil.example"},
		"OpenAI без адреса":           {"provider_id": "openai_compatible"},
		"OpenAI не по http":           {"provider_id": "openai_compatible", "base_url": "ftp://example.com"},
		"OpenAI на частный адрес":     {"provider_id": "openai_compatible", "base_url": "http://10.0.0.5/v1"},
		"OpenAI на метаданные облака": {"provider_id": "openai_compatible", "base_url": "http://169.254.169.254/latest"},
		"OpenAI с паролем в адресе":   {"provider_id": "openai_compatible", "base_url": "https://u:p@example.com/v1"},
		"слишком длинный ключ":        {"provider_id": "anthropic", "api_key": string(bytes.Repeat([]byte("k"), 600))},
		"слишком длинное имя":         {"provider_id": "ollama", "name": string(bytes.Repeat([]byte("я"), 101))},
	}
	for name, body := range cases {
		r, _ := createConfig(t, c, body)
		require.Equal(t, http.StatusUnprocessableEntity, r.Status, "%s: %s", name, r.Body)
	}
}

func TestPrivateProviderURLAllowedWhenAdminPermits(t *testing.T) {
	env := newTestEnvWith(t, func(d *httpapi.Deps) { d.Cfg.AllowPrivateProviderURLs = true })
	c := env.signedIn(t, "u@example.com")
	r, created := createConfig(t, c, map[string]any{"provider_id": "openai_compatible", "base_url": "http://127.0.0.1:8000/v1"})
	require.Equal(t, http.StatusCreated, r.Status, "тело: %s", r.Body)
	require.False(t, created.SendsDataExternally, "локальный vLLM текст наружу не отправляет")
}

func TestKeyRequiresConfiguredSecretKey(t *testing.T) {
	// Сервер без LINGUASTACK_SECRET_KEY не может хранить ключи — и говорит
	// об этом понятно, а не падает. Ollama без ключа работает и так.
	env := newTestEnvWith(t, func(d *httpapi.Deps) { d.Secrets = nil })
	c := env.signedIn(t, "u@example.com")
	r, _ := createConfig(t, c, map[string]any{"provider_id": "anthropic", "api_key": secretKey})
	require.Equal(t, http.StatusServiceUnavailable, r.Status)
	require.Contains(t, string(r.Body), "LINGUASTACK_SECRET_KEY")
	r, _ = createConfig(t, c, map[string]any{"provider_id": "ollama"})
	require.Equal(t, http.StatusCreated, r.Status)
}

func TestPatchAllowExternalAndName(t *testing.T) {
	c := newTestEnv(t).signedIn(t, "u@example.com")
	_, created := createConfig(t, c, map[string]any{"provider_id": "anthropic", "api_key": secretKey})
	require.False(t, created.AllowExternal, "согласие на передачу наружу — только явно (ТЗ §6.4)")

	r := c.do(http.MethodPatch, "/api/provider-configs/"+created.ID, map[string]any{"allow_external": true, "name": "Рабочий"})
	require.Equal(t, http.StatusOK, r.Status)
	var updated configBody
	r.JSON(t, &updated)
	require.True(t, updated.AllowExternal)
	require.Equal(t, "Рабочий", updated.Name)
	require.Equal(t, "9876", *updated.KeyLast4, "ключ при правке не теряется")
}

func TestDeleteConfig(t *testing.T) {
	c := newTestEnv(t).signedIn(t, "u@example.com")
	_, created := createConfig(t, c, map[string]any{"provider_id": "ollama"})
	require.Equal(t, http.StatusNoContent, c.do(http.MethodDelete, "/api/provider-configs/"+created.ID, nil).Status)
	require.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/api/provider-configs/"+created.ID, nil).Status)
}

func TestConfigModelsAndHealthUseDecryptedKey(t *testing.T) {
	var gotKey string
	env := newTestEnvWith(t, func(d *httpapi.Deps) {
		d.BuildProvider = func(_ models.ProviderConfig, apiKey string) (providers.Provider, error) {
			gotKey = apiKey
			return providers.NewFake("echo"), nil
		}
	})
	c := env.signedIn(t, "u@example.com")
	_, created := createConfig(t, c, map[string]any{"provider_id": "anthropic", "api_key": secretKey})

	var list []providers.ModelCapabilities
	r := c.do(http.MethodGet, "/api/provider-configs/"+created.ID+"/models", nil)
	require.Equal(t, http.StatusOK, r.Status, "тело: %s", r.Body)
	r.JSON(t, &list)
	require.Equal(t, "fake-1", list[0].ModelID)
	require.Equal(t, secretKey, gotKey, "адаптер получает расшифрованный ключ")

	var health providers.Health
	c.do(http.MethodGet, "/api/provider-configs/"+created.ID+"/health", nil).JSON(t, &health)
	require.True(t, health.Available)
}

func TestHealthOfRejectedKeyDoesNotLeakIt(t *testing.T) {
	// Сервер провайдера повторяет ключ в ответе об ошибке — он не должен
	// дойти до клиента.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"bad key `+r.Header.Get("X-Api-Key")+`"}}`)
	}))
	t.Cleanup(srv.Close)
	env := newTestEnvWith(t, func(d *httpapi.Deps) {
		d.BuildProvider = func(_ models.ProviderConfig, apiKey string) (providers.Provider, error) {
			return providers.NewAnthropic(apiKey, time.Minute, option.WithBaseURL(srv.URL), option.WithHTTPClient(srv.Client())), nil
		}
	})
	c := env.signedIn(t, "u@example.com")
	_, created := createConfig(t, c, map[string]any{"provider_id": "anthropic", "api_key": secretKey})
	r := c.do(http.MethodGet, "/api/provider-configs/"+created.ID+"/health", nil)
	require.Equal(t, http.StatusOK, r.Status)
	require.NotContains(t, string(r.Body), secretKey)
	require.Contains(t, string(r.Body), `"available":false`)
}

func TestKindModels(t *testing.T) {
	c := newTestEnv(t).signedIn(t, "u@example.com")
	var models []providers.ModelCapabilities
	r := c.do(http.MethodGet, "/api/providers/anthropic/models", nil)
	require.Equal(t, http.StatusOK, r.Status)
	r.JSON(t, &models)
	require.Equal(t, "claude-haiku-5-5", models[0].ModelID, "по умолчанию — самая дешёвая")

	// Список моделей OpenAI-совместимого сервиса зависит от адреса и ключа.
	require.Equal(t, http.StatusUnprocessableEntity, c.do(http.MethodGet, "/api/providers/openai_compatible/models", nil).Status)
	require.Equal(t, http.StatusNotFound, c.do(http.MethodGet, "/api/providers/mystery/models", nil).Status)
}

func TestDeleteConfigInUseIs409(t *testing.T) {
	env := newTestEnv(t)
	c := env.signedIn(t, "u@example.com")
	_, created := createConfig(t, c, map[string]any{"provider_id": "ollama"})
	var owner models.User
	require.NoError(t, env.db.Where("email = ?", "u@example.com").First(&owner).Error)
	cfgID := uuid.MustParse(created.ID)
	testdb.MakeProject(t, env.db, &testdb.ProjectOptions{Owner: &owner.ID, ProviderConfig: &cfgID})

	r := c.do(http.MethodDelete, "/api/provider-configs/"+created.ID, nil)
	require.Equal(t, http.StatusConflict, r.Status, "тело: %s", r.Body)
	require.Equal(t, http.StatusOK, c.do(http.MethodGet, "/api/provider-configs/"+created.ID, nil).Status)
}

func kindIDs(t *testing.T, c *apiClient) []string {
	t.Helper()
	var kinds []struct {
		ID string `json:"provider_id"`
	}
	c.do(http.MethodGet, "/api/providers", nil).JSON(t, &kinds)
	var ids []string
	for _, k := range kinds {
		ids = append(ids, k.ID)
	}
	return ids
}

func TestFakeProviderHiddenByDefault(t *testing.T) {
	// Тестовый провайдер возвращает «RU:» + текст: в рабочей системе он
	// выдал бы мусор за перевод.
	c := newTestEnv(t).signedIn(t, "u@example.com")
	require.NotContains(t, kindIDs(t, c), "fake")
	r, _ := createConfig(t, c, map[string]any{"provider_id": "fake", "name": "F"})
	require.Equal(t, http.StatusUnprocessableEntity, r.Status)
}

func TestFakeProviderAvailableWithFlag(t *testing.T) {
	env := newTestEnvWith(t, func(d *httpapi.Deps) { d.Cfg.E2EFakeProvider = true })
	c := env.signedIn(t, "u@example.com")
	require.Contains(t, kindIDs(t, c), "fake")
	r, cfg := createConfig(t, c, map[string]any{"provider_id": "fake", "name": "F"})
	require.Equal(t, http.StatusCreated, r.Status, "тело: %s", r.Body)
	require.False(t, cfg.SendsDataExternally, "текст никуда не уходит")
	models := c.do(http.MethodGet, "/api/provider-configs/"+cfg.ID+"/models", nil)
	require.Equal(t, http.StatusOK, models.Status)
	require.Contains(t, string(models.Body), "fake-1")
}
