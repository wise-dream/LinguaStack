package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/providers"
	"github.com/vnovels/linguastack/internal/queue"
	"github.com/vnovels/linguastack/internal/security"
	"github.com/vnovels/linguastack/internal/store"
)

const (
	maxAPIKeyLength     = 512
	maxConfigNameLength = 100
)

// defaultBuildProvider собирает адаптер по конфигурации с серверными
// настройками. Тесты подменяют его, чтобы работать с Fake.
func (s *server) defaultBuildProvider(cfg models.ProviderConfig, apiKey string) (providers.Provider, error) {
	return providers.Build(cfg.ProviderID, cfg.BaseURL, apiKey, s.buildOptions())
}

func (s *server) buildOptions() providers.BuildOptions {
	return providers.BuildOptions{
		OllamaBaseURL: s.Cfg.OllamaBaseURL, OllamaNumCtx: s.Cfg.OllamaNumCtx,
		Timeout: s.Cfg.ProviderTimeout, AllowPrivateURLs: s.Cfg.AllowPrivateProviderURLs,
		Fake: s.Cfg.E2EFakeProvider, FakeDelay: s.Cfg.E2EFakeDelay,
	}
}

// ollamaAdminOnlyMessage — ответ, когда Ollama сервера пользователю
// недоступна (LINGUASTACK_OLLAMA_ADMIN_ONLY).
const ollamaAdminOnlyMessage = "Локальные модели сервера (Ollama) доступны только администратору — подключите свой сервис с ключом API"

// serverOllamaFor — может ли пользователь занимать Ollama сервера.
func (s *server) serverOllamaFor(u *models.User) bool {
	return !s.Cfg.OllamaAdminOnly || (u != nil && u.Role == models.RoleAdmin)
}

// kinds — виды провайдеров, доступные пользователю: тестовый — только с
// флагом, Ollama сервера — по serverOllamaFor.
func (s *server) kinds(u *models.User) []providers.Kind {
	var kinds []providers.Kind
	for _, k := range providers.Kinds() {
		if k.ID != "ollama" || s.serverOllamaFor(u) {
			kinds = append(kinds, k)
		}
	}
	if s.Cfg.E2EFakeProvider {
		kinds = append(kinds, providers.FakeKind)
	}
	return kinds
}

func (s *server) lookupKind(u *models.User, id string) (providers.Kind, bool) {
	for _, k := range s.kinds(u) {
		if k.ID == id {
			return k, true
		}
	}
	return providers.Kind{}, false
}

func (s *server) listKinds(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.kinds(UserFromContext(r.Context())))
}

// kindModels — модели вида провайдера без пользовательской конфигурации:
// Ollama сервера и каталог Anthropic. Список OpenAI-совместимого сервиса
// зависит от адреса и ключа — для него есть /provider-configs/{id}/models.
func (s *server) kindModels(w http.ResponseWriter, r *http.Request) {
	kind, ok := s.lookupKind(UserFromContext(r.Context()), r.PathValue("kind"))
	if !ok {
		writeError(w, http.StatusNotFound, "Не найдено")
		return
	}
	switch kind.ID {
	case "ollama":
		p, _ := providers.Build("ollama", "", "", s.buildOptions())
		s.writeModels(w, r, p)
	case "anthropic":
		writeJSON(w, http.StatusOK, providers.AnthropicCatalog())
	default:
		writeError(w, http.StatusUnprocessableEntity, "Список моделей этого провайдера зависит от адреса и ключа — запросите его у конфигурации")
	}
}

func (s *server) writeModels(w http.ResponseWriter, r *http.Request, p providers.Provider) {
	list, err := p.ListModels(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "Провайдер не вернул список моделей: "+err.Error())
		return
	}
	if list == nil {
		list = []providers.ModelCapabilities{}
	}
	writeJSON(w, http.StatusOK, list)
}

type providerConfigIn struct {
	ProviderID    string `json:"provider_id"`
	Name          string `json:"name"`
	BaseURL       string `json:"base_url"`
	APIKey        string `json:"api_key"`
	AllowExternal bool   `json:"allow_external"`
}

// providerConfigOut — ответ без ключа: защита на уровне типа.
type providerConfigOut struct {
	ID                  string    `json:"id"`
	ProviderID          string    `json:"provider_id"`
	Name                string    `json:"name"`
	BaseURL             string    `json:"base_url,omitempty"`
	HasAPIKey           bool      `json:"has_api_key"`
	KeyFingerprint      *string   `json:"key_fingerprint"`
	KeyLast4            *string   `json:"key_last4"`
	AllowExternal       bool      `json:"allow_external"`
	SendsDataExternally bool      `json:"sends_data_externally"`
	CreatedAt           time.Time `json:"created_at"`
}

func configResponse(c *models.ProviderConfig) providerConfigOut {
	out := providerConfigOut{
		ID: c.ID.String(), ProviderID: c.ProviderID, Name: c.Name, BaseURL: c.BaseURL,
		HasAPIKey: c.SecretCiphertext != "", AllowExternal: c.AllowExternal,
		SendsDataExternally: providers.SendsDataExternally(c.ProviderID, c.BaseURL),
		CreatedAt:           c.CreatedAt.UTC(),
	}
	if out.HasAPIKey {
		out.KeyFingerprint, out.KeyLast4 = &c.KeyFingerprint, &c.KeyLast4
	}
	return out
}

// validateConfig возвращает текст ошибки для пользователя или "".
func (s *server) validateConfig(u *models.User, in providerConfigIn) string {
	kind, ok := s.lookupKind(u, in.ProviderID)
	if !ok {
		return "Неизвестный провайдер"
	}
	if utf8.RuneCountInString(in.Name) > maxConfigNameLength {
		return "Название не длиннее 100 символов"
	}
	if len(in.APIKey) > maxAPIKeyLength {
		return "Слишком длинный ключ API"
	}
	if kind.RequiresAPIKey && strings.TrimSpace(in.APIKey) == "" {
		return "Для этого провайдера нужен ключ API"
	}
	if !kind.UserBaseURL && in.BaseURL != "" {
		return "Адрес этого провайдера задаётся сервером"
	}
	if kind.UserBaseURL {
		if err := providers.ValidateBaseURL(in.BaseURL, s.Cfg.AllowPrivateProviderURLs); err != nil {
			return "Недопустимый адрес API: " + err.Error()
		}
	}
	return ""
}

func (s *server) createProviderConfig(w http.ResponseWriter, r *http.Request) {
	var in providerConfigIn
	if !decodeJSON(w, r, &in) {
		return
	}
	in.APIKey = strings.TrimSpace(in.APIKey)
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	if in.ProviderID == "ollama" && !s.serverOllamaFor(UserFromContext(r.Context())) {
		writeError(w, http.StatusForbidden, ollamaAdminOnlyMessage)
		return
	}
	if msg := s.validateConfig(UserFromContext(r.Context()), in); msg != "" {
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	}

	cfg := models.ProviderConfig{
		OwnerID: UserFromContext(r.Context()).ID, ProviderID: in.ProviderID, Name: strings.TrimSpace(in.Name),
		BaseURL: in.BaseURL, AllowExternal: in.AllowExternal && providers.SendsDataExternally(in.ProviderID, in.BaseURL),
	}
	if in.APIKey != "" {
		if s.Secrets == nil {
			writeError(w, http.StatusServiceUnavailable,
				"Сервер не может хранить ключи: не задан LINGUASTACK_SECRET_KEY. Обратитесь к администратору.")
			return
		}
		ciphertext, err := s.Secrets.Encrypt(in.APIKey)
		if err != nil {
			internalError(w, r, err)
			return
		}
		cfg.SecretCiphertext = ciphertext
		cfg.KeyFingerprint = security.Fingerprint(in.APIKey)
		cfg.KeyLast4 = security.Last4(in.APIKey)
	}
	if err := s.DB.WithContext(r.Context()).Create(&cfg).Error; err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, configResponse(&cfg))
}

func (s *server) listProviderConfigs(w http.ResponseWriter, r *http.Request) {
	var list []models.ProviderConfig
	err := s.DB.WithContext(r.Context()).Scopes(store.OwnedScope(UserFromContext(r.Context()).ID)).
		Order("created_at").Find(&list).Error
	if err != nil {
		internalError(w, r, err)
		return
	}
	out := make([]providerConfigOut, len(list))
	for i := range list {
		out[i] = configResponse(&list[i])
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) getProviderConfig(w http.ResponseWriter, r *http.Request) {
	if cfg, ok := loadOwned[models.ProviderConfig](s, w, r, "id"); ok {
		writeJSON(w, http.StatusOK, configResponse(cfg))
	}
}

type providerConfigPatch struct {
	Name          *string `json:"name"`
	AllowExternal *bool   `json:"allow_external"`
}

func (s *server) patchProviderConfig(w http.ResponseWriter, r *http.Request) {
	cfg, ok := loadOwned[models.ProviderConfig](s, w, r, "id")
	if !ok {
		return
	}
	var in providerConfigPatch
	if !decodeJSON(w, r, &in) {
		return
	}
	updates := map[string]any{"updated_at": time.Now().UTC()}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if utf8.RuneCountInString(name) > maxConfigNameLength {
			writeError(w, http.StatusUnprocessableEntity, "Название не длиннее 100 символов")
			return
		}
		updates["name"], cfg.Name = name, name
	}
	if in.AllowExternal != nil {
		allow := *in.AllowExternal && providers.SendsDataExternally(cfg.ProviderID, cfg.BaseURL)
		updates["allow_external"], cfg.AllowExternal = allow, allow
	}
	if err := s.DB.WithContext(r.Context()).Model(cfg).Updates(updates).Error; err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, configResponse(cfg))
}

func (s *server) deleteProviderConfig(w http.ResponseWriter, r *http.Request) {
	cfg, ok := loadOwned[models.ProviderConfig](s, w, r, "id")
	if !ok {
		return
	}
	// Вложенная транзакция: нарушение внешнего ключа откатывает только её.
	err := s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		return tx.Delete(&models.ProviderConfig{}, "id = ?", cfg.ID).Error
	})
	if errors.Is(err, gorm.ErrForeignKeyViolated) {
		writeError(w, http.StatusConflict, "Конфигурацией пользуются проекты перевода — сначала удалите их или смените провайдера")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// providerFor собирает адаптер конфигурации, расшифровывая ключ только
// на время запроса. Ollama сервера — только тем, кому она доступна.
func (s *server) providerFor(u *models.User, cfg *models.ProviderConfig) (providers.Provider, error) {
	if cfg.ProviderID == "ollama" && !s.serverOllamaFor(u) {
		return nil, queue.ErrOllamaAdminOnly
	}
	key := ""
	if cfg.SecretCiphertext != "" {
		if s.Secrets == nil {
			return nil, errSecretsUnavailable
		}
		var err error
		if key, err = s.Secrets.Decrypt(cfg.SecretCiphertext); err != nil {
			return nil, err
		}
	}
	return s.BuildProvider(*cfg, key)
}

var errSecretsUnavailable = errors.New("сервер не может расшифровать ключ: не задан LINGUASTACK_SECRET_KEY")

func (s *server) configModels(w http.ResponseWriter, r *http.Request) {
	cfg, ok := loadOwned[models.ProviderConfig](s, w, r, "id")
	if !ok {
		return
	}
	p, err := s.providerFor(UserFromContext(r.Context()), cfg)
	if errors.Is(err, queue.ErrOllamaAdminOnly) {
		writeError(w, http.StatusForbidden, ollamaAdminOnlyMessage)
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	s.writeModels(w, r, p)
}

func (s *server) configHealth(w http.ResponseWriter, r *http.Request) {
	cfg, ok := loadOwned[models.ProviderConfig](s, w, r, "id")
	if !ok {
		return
	}
	p, err := s.providerFor(UserFromContext(r.Context()), cfg)
	if err != nil {
		writeJSON(w, http.StatusOK, providers.Health{Available: false, Detail: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, p.Health(r.Context()))
}
