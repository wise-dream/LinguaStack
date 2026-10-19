package importers

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"golang.org/x/net/html/charset"
)

// xmlNode — элемент XML деревом. Части OOXML удобнее обходить, чем
// описывать структурами: у Word десятки необязательных обёрток (sdt,
// smartTag, ins, AlternateContent), и текст может прятаться в любой из них.
//
// Имена сравниваются по локальной части: Strict OOXML использует другие
// пространства имён, а смысл элементов тот же.
type xmlNode struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Nodes   []xmlNode  `xml:",any"`
	Text    string     `xml:",chardata"`
}

func (n *xmlNode) local() string { return n.XMLName.Local }

// attr — значение атрибута по локальному имени (w:val, r:id, r:embed).
func (n *xmlNode) attr(local string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func (n *xmlNode) child(local string) *xmlNode {
	if n == nil {
		return nil
	}
	for i := range n.Nodes {
		if n.Nodes[i].XMLName.Local == local {
			return &n.Nodes[i]
		}
	}
	return nil
}

func (n *xmlNode) children(local string) []*xmlNode {
	var out []*xmlNode
	if n == nil {
		return out
	}
	for i := range n.Nodes {
		if n.Nodes[i].XMLName.Local == local {
			out = append(out, &n.Nodes[i])
		}
	}
	return out
}

// path — потомок по цепочке локальных имён.
func (n *xmlNode) path(locals ...string) *xmlNode {
	cur := n
	for _, l := range locals {
		cur = cur.child(l)
	}
	return cur
}

// walk обходит дерево в глубину; false из visit — не заходить внутрь.
func (n *xmlNode) walk(visit func(*xmlNode) bool) {
	if n == nil || !visit(n) {
		return
	}
	for i := range n.Nodes {
		n.Nodes[i].walk(visit)
	}
}

// find — первый потомок с локальным именем.
func (n *xmlNode) find(local string) *xmlNode {
	var found *xmlNode
	n.walk(func(c *xmlNode) bool {
		if found != nil {
			return false
		}
		if c != n && c.local() == local {
			found = c
			return false
		}
		return true
	})
	return found
}

// onOff — переключатель OOXML (<w:b/>, <w:b w:val="0"/>): present — элемент
// есть, value — его значение.
func (n *xmlNode) onOff() (present, value bool) {
	if n == nil {
		return false, false
	}
	switch strings.ToLower(n.attr("val")) {
	case "0", "false", "off", "none":
		return true, false
	}
	return true, true
}

func newXMLDecoder(data []byte) *xml.Decoder {
	// encoding/xml не загружает внешние сущности и DTD (XXE) и не
	// разворачивает пользовательские сущности.
	d := xml.NewDecoder(bytes.NewReader(data))
	d.CharsetReader = charset.NewReaderLabel
	return d
}

func parseXMLNode(data []byte) (*xmlNode, error) {
	var n xmlNode
	if err := newXMLDecoder(data).Decode(&n); err != nil {
		return nil, err
	}
	return &n, nil
}

// streamBody передаёт visit каждый элемент верхнего уровня w:body по
// отдельности: в памяти одновременно только один абзац или таблица, а не
// весь документ. Ошибка разбора возвращается после уже переданных
// элементов — прочитанное до повреждения сохраняется.
func streamBody(data []byte, visit func(*xmlNode)) error {
	d := newXMLDecoder(data)
	inBody := false
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			if inBody {
				return io.ErrUnexpectedEOF
			}
			return nil
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if !inBody {
				inBody = t.Name.Local == "body"
				continue
			}
			var n xmlNode
			if err := d.DecodeElement(&n, &t); err != nil {
				return err
			}
			visit(&n)
		case xml.EndElement:
			if inBody && t.Name.Local == "body" {
				return nil
			}
		}
	}
}
