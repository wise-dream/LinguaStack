package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/config"
	"github.com/vnovels/linguastack/internal/httpapi"
	"github.com/vnovels/linguastack/internal/importers"
	"github.com/vnovels/linguastack/internal/security"
	"github.com/vnovels/linguastack/internal/storage"
	"github.com/vnovels/linguastack/internal/testdb"
)

const testPassword = "correct-horse-battery"

// testEnv — настоящий HTTP-сервер поверх транзакции теста: обработчики и
// проверки теста видят одни данные, а откат убирает всё после теста.
type testEnv struct {
	srv     *httptest.Server
	db      *gorm.DB
	cfg     config.Settings
	storage string // корень файлового хранилища теста
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWith(t, nil)
}

// newTestEnvWith позволяет подменить зависимости, например сборку
// провайдера на Fake или отсутствие ключа шифрования.
func newTestEnvWith(t *testing.T, adjust func(*httpapi.Deps)) *testEnv {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)
	// Тестовый сервер работает по http: cookie с Secure клиент не вернёт.
	cfg.SessionCookieSecure = false
	tx := testdb.New(t)
	root := t.TempDir()
	store, err := storage.New(root, cfg.MaxUploadBytes)
	require.NoError(t, err)
	key, err := config.Settings{SecretKey: config.GenerateSecretKey()}.RequireSecretKey()
	require.NoError(t, err)
	box, err := security.NewSecretBox(key)
	require.NoError(t, err)
	deps := httpapi.Deps{Cfg: cfg, DB: tx, Store: store, Importers: importers.Default(cfg), Secrets: box}
	if adjust != nil {
		adjust(&deps)
	}
	srv := httptest.NewServer(httpapi.NewServer(deps))
	// Cleanup выполняется в обратном порядке: сервер закроется раньше,
	// чем откатится транзакция, которой он пользуется.
	t.Cleanup(srv.Close)
	return &testEnv{srv: srv, db: tx, cfg: cfg, storage: root}
}

// upload отправляет файл как multipart-форму с полем file.
func (c *apiClient) upload(filename string, content []byte) *response {
	c.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", filename)
	require.NoError(c.t, err)
	_, err = part.Write(content)
	require.NoError(c.t, err)
	require.NoError(c.t, mw.Close())
	return c.do(http.MethodPost, "/api/books", body.Bytes(), [2]string{"Content-Type", mw.FormDataContentType()})
}

// apiClient — клиент со своей cookie jar, то есть отдельный «браузер».
type apiClient struct {
	t    *testing.T
	http *http.Client
	base string
}

func (e *testEnv) client(t *testing.T) *apiClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &apiClient{t: t, http: &http.Client{Jar: jar}, base: e.srv.URL}
}

type response struct {
	Status int
	Header http.Header
	Body   []byte
}

func (r *response) JSON(t *testing.T, v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal(r.Body, v), "тело: %s", r.Body)
}

func (c *apiClient) do(method, path string, body any, headers ...[2]string) *response {
	c.t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		require.NoError(c.t, err)
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(c.t.Context(), method, c.base+path, reader)
	require.NoError(c.t, err)
	if reader != nil {
		if _, raw := body.([]byte); !raw {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}
	resp, err := c.http.Do(req)
	require.NoError(c.t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(c.t, err)
	return &response{Status: resp.StatusCode, Header: resp.Header, Body: raw}
}

func (c *apiClient) cookie(name string) string {
	c.t.Helper()
	u, err := url.Parse(c.base)
	require.NoError(c.t, err)
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == name {
			return ck.Value
		}
	}
	return ""
}

func (c *apiClient) register(email string) *response {
	c.t.Helper()
	return c.do(http.MethodPost, "/api/auth/register", map[string]string{"email": email, "password": testPassword})
}

func (c *apiClient) login(email, password string) *response {
	c.t.Helper()
	return c.do(http.MethodPost, "/api/auth/login", map[string]string{"email": email, "password": password})
}

// signedIn регистрирует и логинит нового пользователя.
func (e *testEnv) signedIn(t *testing.T, email string) *apiClient {
	t.Helper()
	c := e.client(t)
	require.Equal(t, http.StatusCreated, c.register(email).Status)
	require.Equal(t, http.StatusOK, c.login(email, testPassword).Status)
	return c
}
