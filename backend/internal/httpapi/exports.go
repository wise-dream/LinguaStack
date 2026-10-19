package httpapi

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/vnovels/linguastack/internal/db/models"
	"github.com/vnovels/linguastack/internal/exporters"
	"github.com/vnovels/linguastack/internal/store"
)

// Экспорт перевода (ТЗ §3.2, §12; дополнение §16): задача ставится здесь,
// файл собирает worker, скачивание — после проверки владения.

type exportIn struct {
	TranslationID string `json:"translation_id"`
	Format        string `json:"format"`
	Mode          string `json:"mode"`
}

type exportOut struct {
	ID            uuid.UUID  `json:"id"`
	BookID        uuid.UUID  `json:"book_id"`
	TranslationID uuid.UUID  `json:"translation_id"`
	Format        string     `json:"format"`
	Mode          string     `json:"mode"`
	Status        string     `json:"status"`
	FileName      string     `json:"file_name"`
	SizeBytes     int64      `json:"size_bytes"`
	Untranslated  int        `json:"untranslated"`
	Error         string     `json:"error,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

func exportResponse(j *models.ExportJob) exportOut {
	out := exportOut{ID: j.ID, BookID: j.BookID, TranslationID: j.ProjectID, Format: j.Format, Mode: j.Mode,
		Status: j.Status, SizeBytes: j.SizeBytes, Untranslated: j.Untranslated, Error: j.Error,
		CreatedAt: j.CreatedAt.UTC(), FinishedAt: j.FinishedAt}
	if j.Status == models.ExportCompleted {
		out.FileName = j.BaseName + j.FileExt
	}
	return out
}

const maxExportBaseName = 100 // символов в имени файла без расширения

// exportBaseName — «Название (язык)» без символов, опасных в именах файлов и
// заголовках: управляющих, разделителей путей, кавычек.
func exportBaseName(title, lang string) string {
	clean := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return ' '
		}
		return r
	}, title)
	clean = strings.Trim(strings.Join(strings.Fields(clean), " "), ". ")
	for utf8.RuneCountInString(clean) > maxExportBaseName {
		_, size := utf8.DecodeLastRuneInString(clean)
		clean = clean[:len(clean)-size]
	}
	if clean == "" {
		clean = "book"
	}
	return clean + " (" + lang + ")"
}

// contentDisposition — «attachment» с именем в RFC 5987 (UTF-8) и
// ASCII-запасным для старых клиентов. Имя кодируется целиком: перевод
// строки или кавычка не выйдут за пределы заголовка.
func contentDisposition(name string) string {
	fallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' || r == '%' {
			return '_'
		}
		return r
	}, name)
	return `attachment; filename="` + fallback + `"; filename*=UTF-8''` + rfc5987(name)
}

// rfc5987 кодирует значение по RFC 5987: как есть остаются только
// attr-char, всё остальное — %XX по байтам UTF-8.
func rfc5987(s string) string {
	const attrChar = "!#$&+-.^_`|~"
	var b strings.Builder
	for _, c := range []byte(s) {
		if c < utf8.RuneSelf && (unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c)) || strings.IndexByte(attrChar, c) >= 0) {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func validExportMode(mode string) bool {
	return mode == string(exporters.ModeTranslation) || mode == string(exporters.ModeBilingual)
}

func (s *server) createExport(w http.ResponseWriter, r *http.Request) {
	book, ok := loadOwned[models.Book](s, w, r, "id")
	if !ok {
		return
	}
	var in exportIn
	if !decodeJSON(w, r, &in) {
		return
	}
	if _, ok := exporters.Get(in.Format); !ok {
		writeError(w, http.StatusUnprocessableEntity, "format: один из "+strings.Join(exporters.Formats(), ", "))
		return
	}
	if !validExportMode(in.Mode) {
		writeError(w, http.StatusUnprocessableEntity, "mode: translation или bilingual")
		return
	}
	user := UserFromContext(r.Context())
	projectID, err := uuid.Parse(in.TranslationID)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "Перевод не найден")
		return
	}
	project, err := store.Owned[models.TranslationProject](r.Context(), s.DB, projectID, user.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnprocessableEntity, "Перевод не найден")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	if project.BookID != book.ID {
		writeError(w, http.StatusUnprocessableEntity, "Перевод относится к другой книге")
		return
	}

	var active int64
	err = s.DB.WithContext(r.Context()).Model(&models.ExportJob{}).
		Where("owner_id = ? AND status IN ?", user.ID, []string{models.ExportPending, models.ExportRunning}).
		Count(&active).Error
	if err != nil {
		internalError(w, r, err)
		return
	}
	if limit := s.Cfg.MaxActiveExportsPerUser; limit > 0 && active >= int64(limit) {
		writeError(w, http.StatusTooManyRequests, "Слишком много экспортов в работе: дождитесь, пока они завершатся")
		return
	}

	job := models.ExportJob{OwnerID: user.ID, BookID: book.ID, ProjectID: project.ID, Format: in.Format, Mode: in.Mode,
		Status: models.ExportPending, BaseName: exportBaseName(book.Title, project.TargetLang)}
	if err := s.DB.WithContext(r.Context()).Create(&job).Error; err != nil {
		internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, exportResponse(&job))
}

const maxListedExports = 100

func (s *server) listBookExports(w http.ResponseWriter, r *http.Request) {
	book, ok := loadOwned[models.Book](s, w, r, "id")
	if !ok {
		return
	}
	var jobs []models.ExportJob
	err := s.DB.WithContext(r.Context()).Scopes(store.OwnedScope(book.OwnerID)).
		Where("book_id = ?", book.ID).Order("created_at DESC, id").Limit(maxListedExports).Find(&jobs).Error
	if err != nil {
		internalError(w, r, err)
		return
	}
	out := make([]exportOut, len(jobs))
	for i := range jobs {
		out[i] = exportResponse(&jobs[i])
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) getExport(w http.ResponseWriter, r *http.Request) {
	if job, ok := loadOwned[models.ExportJob](s, w, r, "id"); ok {
		writeJSON(w, http.StatusOK, exportResponse(job))
	}
}

// exportCSP — экспортированный HTML открывается как вложение, но и при
// открытии в браузере не исполняется на нашем origin.
const exportCSP = "default-src 'none'; img-src data:; style-src 'unsafe-inline'; sandbox"

func (s *server) downloadExport(w http.ResponseWriter, r *http.Request) {
	job, ok := loadOwned[models.ExportJob](s, w, r, "id")
	if !ok {
		return
	}
	if job.Status != models.ExportCompleted {
		writeError(w, http.StatusConflict, "Экспорт ещё не готов")
		return
	}
	f, err := s.Store.OpenExport(job.OwnerID, job.BookID, job.ID, job.FileExt)
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusNotFound, "Файл экспорта не найден: запустите экспорт снова")
		return
	}
	if err != nil {
		internalError(w, r, err)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		internalError(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", job.ContentType)
	h.Set("Content-Disposition", contentDisposition(job.BaseName+job.FileExt))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", exportCSP)
	h.Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "", info.ModTime(), f)
}

func (s *server) deleteExport(w http.ResponseWriter, r *http.Request) {
	job, ok := loadOwned[models.ExportJob](s, w, r, "id")
	if !ok {
		return
	}
	if job.Status == models.ExportRunning {
		writeError(w, http.StatusConflict, "Экспорт выполняется: удалите его после завершения")
		return
	}
	err := s.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&models.ExportJob{}, "id = ?", job.ID).Error; err != nil {
			return err
		}
		if job.FileExt == "" {
			return nil
		}
		return s.Store.DeleteExport(job.OwnerID, job.BookID, job.ID, job.FileExt)
	})
	if err != nil {
		internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
