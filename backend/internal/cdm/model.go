package cdm

import "fmt"

// BlockType — тип структурного блока.
type BlockType string

const (
	Heading       BlockType = "heading"
	Paragraph     BlockType = "paragraph"
	ListItem      BlockType = "list_item"
	TableCell     BlockType = "table_cell"
	Caption       BlockType = "caption"
	Blockquote    BlockType = "blockquote"
	CodeBlock     BlockType = "code_block"
	MathBlock     BlockType = "math_block"
	Image         BlockType = "image"
	ThematicBreak BlockType = "thematic_break"
	RawHTML       BlockType = "raw_html"
)

// IsTranslatable — попадает ли блок в перевод. Код, формулы, изображения
// и сырой HTML в модель не уходят вовсе (ТЗ §5.3, дополнение §2).
func IsTranslatable(t BlockType) bool {
	switch t {
	case Heading, Paragraph, ListItem, TableCell, Caption, Blockquote:
		return true
	default:
		return false
	}
}

// Block — структурный блок документа.
type Block struct {
	NodeID     string            `json:"node_id"`
	Type       BlockType         `json:"type"`
	Text       string            `json:"text"`
	Level      int               `json:"level,omitempty"`
	SourceHash string            `json:"source_hash"`
	Meta       map[string]string `json:"meta,omitempty"`
}

// Chapter — глава или секция; вложенность задаёт оглавление.
type Chapter struct {
	Index    int       `json:"index"`
	Title    string    `json:"title"`
	Blocks   []Block   `json:"blocks"`
	Children []Chapter `json:"children,omitempty"`
}

// BookMeta — метаданные книги.
type BookMeta struct {
	Title        string `json:"title"`
	Author       string `json:"author,omitempty"`
	Language     string `json:"language,omitempty"`
	SourceFormat string `json:"source_format"`
}

// Document — книга в каноническом представлении.
type Document struct {
	Meta     BookMeta  `json:"meta"`
	Chapters []Chapter `json:"chapters"`
}

// TOCEntry — элемент оглавления.
type TOCEntry struct {
	Index    int        `json:"index"`
	Title    string     `json:"title"`
	Children []TOCEntry `json:"children,omitempty"`
}

func walkChapters(chapters []Chapter, visit func(*Chapter)) {
	for i := range chapters {
		visit(&chapters[i])
		walkChapters(chapters[i].Children, visit)
	}
}

// Blocks — все блоки в порядке чтения: собственные блоки главы, затем
// блоки её подглав.
func (d Document) Blocks() []Block {
	var out []Block
	walkChapters(d.Chapters, func(ch *Chapter) { out = append(out, ch.Blocks...) })
	return out
}

// TranslatableBlocks — блоки, которые уходят в перевод.
func (d Document) TranslatableBlocks() []Block {
	var out []Block
	for _, b := range d.Blocks() {
		if IsTranslatable(b.Type) {
			out = append(out, b)
		}
	}
	return out
}

// TOC — оглавление.
func (d Document) TOC() []TOCEntry {
	return tocOf(d.Chapters)
}

func tocOf(chapters []Chapter) []TOCEntry {
	if len(chapters) == 0 {
		return nil
	}
	out := make([]TOCEntry, len(chapters))
	for i, ch := range chapters {
		out[i] = TOCEntry{Index: ch.Index, Title: ch.Title, Children: tocOf(ch.Children)}
	}
	return out
}

// Validate проверяет инварианты: индексы глав и идентификаторы блоков
// уникальны — на них ссылаются сегменты перевода.
func (d Document) Validate() error {
	chapters := map[int]bool{}
	blocks := map[string]bool{}
	var err error
	walkChapters(d.Chapters, func(ch *Chapter) {
		if err != nil {
			return
		}
		if chapters[ch.Index] {
			err = fmt.Errorf("повторяющийся индекс главы %d", ch.Index)
			return
		}
		chapters[ch.Index] = true
		for _, b := range ch.Blocks {
			if blocks[b.NodeID] {
				err = fmt.Errorf("повторяющийся идентификатор блока %s", b.NodeID)
				return
			}
			blocks[b.NodeID] = true
		}
	})
	return err
}
