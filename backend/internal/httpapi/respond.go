package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// maxJSONBody — предел тела JSON-запроса. Загрузка книг идёт отдельным
// путём со своим лимитом (ТЗ §13).
const maxJSONBody = 64 << 10

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Warn("не удалось записать ответ", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// internalError логирует причину и отдаёт клиенту обезличенную ошибку:
// текст внутренних ошибок может содержать SQL, пути и прочие детали.
func internalError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("внутренняя ошибка", "method", r.Method, "path", r.URL.Path, "error", err)
	writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
}

// decodeJSON читает тело запроса строго: ограниченный размер, только
// известные поля, ровно один JSON-объект. Неизвестные поля отвергаются,
// чтобы, например, "role": "admin" нельзя было протащить в регистрацию.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "Тело запроса слишком большое")
			return false
		}
		writeError(w, http.StatusUnprocessableEntity, "Некорректное тело запроса")
		return false
	}
	if dec.More() {
		writeError(w, http.StatusUnprocessableEntity, "Некорректное тело запроса")
		return false
	}
	return true
}
