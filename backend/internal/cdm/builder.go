package cdm

// Builder собирает Document по мере разбора исходника.
//
// Выдача индексов глав, идентификаторов и хешей блоков сосредоточена
// здесь, чтобы импортёры разных форматов не дублировали эту логику и не
// расходились в ней.
type Builder struct {
	meta      BookMeta
	roots     []*chapterNode
	stack     []*chapterNode // открытые главы, по одной на уровень глубины
	nextIndex int
}

type chapterNode struct {
	chapter  Chapter
	depth    int
	children []*chapterNode
}

// NewBuilder начинает новый документ.
func NewBuilder(meta BookMeta) *Builder {
	return &Builder{meta: meta}
}

// StartChapter открывает главу на глубине depth (1 — верхний уровень).
// Пропуск уровней (h1 → h3) допустим: глава вкладывается в ближайшую
// открытую главу меньшей глубины.
func (b *Builder) StartChapter(title string, depth int) {
	if depth < 1 {
		depth = 1
	}
	for len(b.stack) > 0 && b.stack[len(b.stack)-1].depth >= depth {
		b.stack = b.stack[:len(b.stack)-1]
	}
	node := &chapterNode{
		chapter: Chapter{Index: b.nextIndex, Title: title, Blocks: []Block{}},
		depth:   depth,
	}
	b.nextIndex++
	if len(b.stack) == 0 {
		b.roots = append(b.roots, node)
	} else {
		parent := b.stack[len(b.stack)-1]
		parent.children = append(parent.children, node)
	}
	b.stack = append(b.stack, node)
}

// Add добавляет блок в текущую главу и возвращает его. Если глава ещё не
// открыта (текст до первого заголовка, документ без заголовков), создаётся
// неявная глава с названием книги.
func (b *Builder) Add(t BlockType, text string, level int, meta map[string]string) Block {
	if len(b.stack) == 0 {
		b.StartChapter(b.meta.Title, 1)
	}
	current := &b.stack[len(b.stack)-1].chapter
	if len(meta) == 0 {
		meta = nil
	}
	block := Block{
		NodeID:     NodeID(current.Index, len(current.Blocks)),
		Type:       t,
		Text:       text,
		Level:      level,
		SourceHash: ContentHash(text),
		Meta:       meta,
	}
	current.Blocks = append(current.Blocks, block)
	return block
}

// Build возвращает собранный документ.
func (b *Builder) Build() Document {
	return Document{Meta: b.meta, Chapters: buildChapters(b.roots)}
}

func buildChapters(nodes []*chapterNode) []Chapter {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]Chapter, len(nodes))
	for i, n := range nodes {
		out[i] = n.chapter
		out[i].Children = buildChapters(n.children)
	}
	return out
}
