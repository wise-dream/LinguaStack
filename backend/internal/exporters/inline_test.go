package exporters

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInlineHTMLEscapes(t *testing.T) {
	cases := map[string]string{
		`<script>alert(1)</script> & "q"`:      `alert(1) &amp; &#34;q&#34;`,
		`[x](javascript:alert(1))`:             `x`,
		`[глава](ch2.xhtml#a)`:                 `глава`,
		`[сайт](https://example.com/?a=1&b=2)`: `<a href="https://example.com/?a=1&amp;b=2">сайт</a>`,
		`[почта](mailto:a@example.com)`:        `<a href="mailto:a@example.com">почта</a>`,
		`<https://example.com>`:                `<a href="https://example.com">https://example.com</a>`,
		`*a* **b** ` + "`<c>`" + ` \*d\*`:      `<em>a</em> <strong>b</strong> <code>&lt;c&gt;</code> *d*`,
		`a &amp; b &lt;`:                       `a &amp; b &lt;`,
		"плохой\x02символ\x0c":                 "плохойсимвол",
	}
	for in, want := range cases {
		require.Equal(t, want, renderInlineHTML(in, inlineOptions{}), in)
	}
}

func TestInlineHTMLBreaks(t *testing.T) {
	require.Equal(t, "раз<br/>два", renderInlineHTML("раз\nдва", inlineOptions{XHTML: true, HardBreaks: true}))
	require.Equal(t, "раз<br>два", renderInlineHTML("раз\nдва", inlineOptions{HardBreaks: true}))
	require.Equal(t, "раз два", renderInlineHTML("раз\nдва", inlineOptions{XHTML: true}))
}

func TestInlineParagraphOnly(t *testing.T) {
	for _, in := range []string{"- не список", "# не заголовок", "1. не список", "> не цитата", "    не код"} {
		require.NotContains(t, renderInlineHTML(in, inlineOptions{}), "<", in)
	}
	require.Equal(t, "[1]: https://example.com", renderInlineHTML(`\[1\]: https://example.com`, inlineOptions{}))
}

func TestInlineImages(t *testing.T) {
	opts := inlineOptions{XHTML: true, Image: func(src string) (string, bool) {
		if src == "OEBPS/a.png" {
			return "images/img0001.png", true
		}
		return "", false
	}}
	require.Equal(t, `до <img src="images/img0001.png" alt="схема"/> после`, renderInlineHTML("до ![схема](OEBPS/a.png) после", opts))
	require.Equal(t, `до нет после`, renderInlineHTML("до ![нет](gone.png) после", opts), "недоступная картинка — её подпись")
}

func TestInlinePlain(t *testing.T) {
	require.Equal(t, "a b c [d] x", inlinePlain("*a* **b** `c` \\[d\\] [x](http://e.com)", false))
	require.Equal(t, "раз\nдва", inlinePlain("раз\nдва", true))
	require.Equal(t, "раз два", inlinePlain("раз\nдва", false))
	require.Equal(t, "[Изображение: схема]", inlinePlain("![схема](a.png)", false))
}

func TestXMLSafe(t *testing.T) {
	require.Equal(t, "a\tb\nв", xmlSafe("a\x00\x02\x0c\tb\nв￾"))
}
