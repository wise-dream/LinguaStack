# PDF-фикстуры

`outline.pdf`, `plain.pdf` и `scanned.pdf` печатает из HTML headless Chrome:
`./generate.sh` (нужны `google-chrome` и шрифты DejaVu). Это обычный путь,
которым получаются реальные PDF «из браузера»: встроенные подмножества
шрифтов, ToUnicode, закладки из заголовков (`--generate-pdf-document-outline`
только для `outline.pdf`).

| Файл | Что проверяет |
|---|---|
| `outline.pdf` | закладки → главы; кириллица; абзацы на несколько строк; мягкие переносы; список; код моноширинным шрифтом; колонтитул и номер страницы на каждой из трёх страниц |
| `plain.pdf` | тот же текст без закладок — главы по заголовкам |
| `scanned.pdf` | страница из одной картинки с текстом — нет текстового слоя |
| `encrypted.pdf` | `plain.pdf`, зашифрованный паролем пользователя «x» |

Chrome не шифрует PDF, поэтому `encrypted.pdf` получен один раз утилитой
pdfcpu v0.16.0 из временного модуля (в `go.mod` проекта её нет):

```bash
GOBIN=/tmp/tools go install github.com/pdfcpu/pdfcpu/cmd/pdfcpu@v0.16.0
/tmp/tools/pdfcpu encrypt --upw x --opw owner plain.pdf encrypted.pdf
```
