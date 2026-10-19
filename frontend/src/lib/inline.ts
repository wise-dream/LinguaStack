// Встроенная разметка текста книги → безопасный HTML. Единственный путь,
// которым текст книги попадает в v-html: сначала всё экранируется, потом
// из экранированного текста собираются только <em>, <strong>, <code> и
// внешние ссылки. Импорт приводит разметку всех форматов к Markdown,
// поэтому здесь — только он.

const OPEN = String.fromCharCode(0xe000)
const CLOSE = String.fromCharCode(0xe001)
const slot = (i: number) => `${OPEN}${i}${CLOSE}`
const slotRe = new RegExp(`${OPEN}(\\d+)${CLOSE}`, 'g')

const html: Record<string, string> = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }

export function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => html[c] ?? c)
}

const codeRe = /`([^`]+)`/g
const escapeRe = /\\([\\`*_[\]()#+\-.!{}<>|~])/g
const linkRe = /\[([^\]\n]+)\]\(([^)\s]+)\)/g
const externalRe = /^(https?:|mailto:)/i
const boldRe = /\*\*(?=\S)(.+?)(?<=\S)\*\*/g
const starItalicRe = /(^|[^\w*])\*(?=\S)(.+?)(?<=\S)\*(?![\w*])/g
const underscoreItalicRe = /(^|[^\w])_(?=\S)(.+?)(?<=\S)_(?!\w)/g

function emphasis(s: string): string {
  return s.replace(boldRe, '<strong>$1</strong>').replace(starItalicRe, '$1<em>$2</em>').replace(underscoreItalicRe, '$1<em>$2</em>')
}

/** Текст вне `кода`: экранирование, ссылки, выделения. */
function renderText(raw: string, slots: string[]): string {
  const keep = (htmlPart: string) => {
    slots.push(htmlPart)
    return slot(slots.length - 1)
  }
  let s = raw.replace(escapeRe, (_, ch: string) => keep(escapeHtml(ch)))
  s = escapeHtml(s)
  // Ссылки прячутся в слоты до выделений: иначе «_» и «*» в адресе
  // превратились бы в разметку внутри атрибута.
  s = s.replace(linkRe, (_, text: string, url: string) => {
    const label = emphasis(text)
    // Адрес уже экранирован: кавычка стала &quot; и из атрибута не выйдет.
    // Не внешний адрес — только текст: другие схемы опасны, а
    // относительные ссылки книги в адресах приложения ведут в никуда.
    return keep(externalRe.test(url) ? `<a href="${url}" rel="noopener noreferrer" target="_blank">${label}</a>` : label)
  })
  return emphasis(s)
}

export function renderInline(text: string): string {
  const slots: string[] = []
  let out = ''
  let last = 0
  for (const m of text.matchAll(codeRe)) {
    out += renderText(text.slice(last, m.index), slots)
    out += `<code>${escapeHtml(m[1] ?? '')}</code>`
    last = (m.index ?? 0) + m[0].length
  }
  out += renderText(text.slice(last), slots)
  // Слоты могут вкладываться (выделение внутри ссылки): раскрываем до конца.
  let prev = ''
  while (prev !== out) {
    prev = out
    out = out.replace(slotRe, (_, i: string) => slots[Number(i)] ?? '')
  }
  return out
}
