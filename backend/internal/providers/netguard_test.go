package providers_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/providers"
)

func TestValidateBaseURL(t *testing.T) {
	ok := []string{"https://api.openai.com/v1", "https://openrouter.ai/api/v1", "http://llm.example.com:8000/v1"}
	for _, u := range ok {
		require.NoError(t, providers.ValidateBaseURL(u, false), u)
	}
	bad := map[string]string{
		"":                              "пустой",
		"ftp://example.com":             "не http(s)",
		"https://":                      "без хоста",
		"https://user:pass@example.com": "учётные данные в адресе хранились бы открыто",
		"http://127.0.0.1:8000/v1":      "loopback",
		"http://localhost:1234/v1":      "localhost",
		"http://10.0.0.5/v1":            "частная сеть",
		"http://192.168.1.10/v1":        "частная сеть",
		"http://172.16.0.1/v1":          "частная сеть",
		"http://169.254.169.254/latest": "метаданные облака",
		"http://[::1]:8000":             "loopback IPv6",
		"http://[fd00::1]":              "unique local IPv6",
		"http://100.64.0.1":             "CGNAT",
		"http://0.0.0.0:80":             "unspecified",
	}
	for u, why := range bad {
		require.Error(t, providers.ValidateBaseURL(u, false), "%s: %s", u, why)
	}
}

func TestValidateBaseURLAllowsPrivateWhenPermitted(t *testing.T) {
	// Администратор может разрешить локальный vLLM явно.
	require.NoError(t, providers.ValidateBaseURL("http://127.0.0.1:8000/v1", true))
	require.Error(t, providers.ValidateBaseURL("http://169.254.169.254/", true),
		"метаданные облака недоступны даже при разрешённых частных адресах")
	require.Error(t, providers.ValidateBaseURL("ftp://127.0.0.1", true))
}

func TestGuardedClientBlocksPrivateAddressAtDialTime(t *testing.T) {
	// DNS-rebinding: имя при проверке указывало наружу, при запросе —
	// внутрь. Закрывается только проверкой фактического IP при соединении.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "внутренний сервис")
	}))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	blockedResp, err := providers.GuardedHTTPClient(false).Do(req)
	if blockedResp != nil {
		_ = blockedResp.Body.Close() // при сломанной защите ответ всё же придёт
	}
	require.Error(t, err)
	require.Contains(t, err.Error(), "запрещён")

	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	resp, err := providers.GuardedHTTPClient(true).Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

func TestBuildProviders(t *testing.T) {
	opts := providers.BuildOptions{OllamaBaseURL: "http://localhost:11434", OllamaNumCtx: 8192}
	o, err := providers.Build("ollama", "", "", opts)
	require.NoError(t, err)
	require.Equal(t, "ollama", o.ID())

	a, err := providers.Build("anthropic", "", "sk-ant-key-123456789", opts)
	require.NoError(t, err)
	require.Equal(t, "anthropic", a.ID())
	_, err = providers.Build("anthropic", "", "", opts)
	require.Error(t, err, "Anthropic без ключа не собирается")

	c, err := providers.Build("openai_compatible", "https://api.example.com/v1", "", opts)
	require.NoError(t, err)
	require.True(t, c.SendsDataExternally())
	_, err = providers.Build("openai_compatible", "http://127.0.0.1:9/v1", "", opts)
	require.Error(t, err, "частный адрес запрещён без разрешения администратора")

	_, err = providers.Build("mystery", "", "", opts)
	require.ErrorIs(t, err, providers.ErrUnknownProvider)
}

func TestKindsCatalog(t *testing.T) {
	kinds := providers.Kinds()
	byID := map[string]providers.Kind{}
	for _, k := range kinds {
		byID[k.ID] = k
	}
	require.False(t, byID["ollama"].SendsDataExternally)
	require.True(t, byID["anthropic"].SendsDataExternally)
	require.True(t, byID["anthropic"].RequiresAPIKey)
	require.True(t, byID["openai_compatible"].RequiresBaseURL)
	require.False(t, byID["ollama"].UserBaseURL, "адрес Ollama задаёт администратор")
}
