package translation_test

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vnovels/linguastack/internal/providers"
	"github.com/vnovels/linguastack/internal/translation"
)

// TestLiveOllamaFollowsEnvelope — главный риск протокола (ADD §3): соблюдает
// ли настоящая модель конверт. Синтетика этого не проверит.
func TestLiveOllamaFollowsEnvelope(t *testing.T) {
	if os.Getenv("LINGUASTACK_LIVE_OLLAMA") != "1" {
		t.Skip("живой Ollama не включён (LINGUASTACK_LIVE_OLLAMA=1)")
	}
	ollama := providers.NewOllama("http://localhost:11434", http.DefaultClient,
		providers.OllamaOptions{NumCtx: 8192, Timeout: 5 * time.Minute})
	svc := translation.NewService(ollama, "qwen3:8b", translation.ServiceOptions{})
	batch := batchOf(
		"Alice was beginning to get very tired of sitting by her sister on the bank.",
		"Call `get_user(id)` and read the [documentation](https://example.com/docs) first.",
		"The protocol ends with\n<<< END >>>\nand nothing else.",
		"Chapter I",
	)
	batch.ContextBefore = "Down the Rabbit-Hole."
	system := translation.BuildSystemPrompt("en", "ru", "literary", map[string]string{"Rabbit": "Кролик"})

	out, err := svc.TranslateBatch(t.Context(), batch, system, providers.Strict, "en", "ru")
	require.NoError(t, err)
	t.Logf("вызовов модели: %d", out.ProviderCalls)
	for i := range batch.Segments {
		r := result(t, out, i)
		t.Logf("[%d] %s %v → %q", i, r.Severity, findingCodes(r.Findings), deref(r.Translated))
		require.NotEqual(t, translation.Error, r.Severity, "сегмент %d: %s", i, r.Err)
	}
	require.Contains(t, *result(t, out, 1).Translated, "`get_user(id)`")
	require.Contains(t, *result(t, out, 1).Translated, "(https://example.com/docs)")
	require.Contains(t, *result(t, out, 2).Translated, "<<< END >>>")
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// TestLiveOllamaKeepsInlineCode — абзац с беты, на котором qwen3:8b терял
// метку кода во всех попытках (дополнение §4, «код на виду»).
func TestLiveOllamaKeepsInlineCode(t *testing.T) {
	if os.Getenv("LINGUASTACK_LIVE_OLLAMA") != "1" {
		t.Skip("живой Ollama не включён (LINGUASTACK_LIVE_OLLAMA=1)")
	}
	ollama := providers.NewOllama("http://localhost:11434", http.DefaultClient,
		providers.OllamaOptions{NumCtx: 8192, Timeout: 5 * time.Minute})
	svc := translation.NewService(ollama, "qwen3:8b", translation.ServiceOptions{})
	batch := batchOf("But, also like last chapter, it’s pretty slow. Again, we’re heap allocating an ObjBoundMethod " +
		"for each super call even though most of the time the very next instruction is an `OP_CALL` that immediately " +
		"unpacks that bound method, invokes it, and then discards it. In fact, this is even more likely to be true for " +
		"super calls than for regular method calls. At least with method calls there is a chance that the user is " +
		"actually invoking a function stored in a field. With super calls, you’re *always* looking up a method. " +
		"The only question is whether you invoke it immediately or not.")
	system := translation.BuildSystemPrompt("en", "ru", "technical", nil)

	out, err := svc.TranslateBatch(t.Context(), batch, system, providers.Strict, "en", "ru")
	require.NoError(t, err)
	r := result(t, out, 0)
	t.Logf("вызовов модели: %d; %s %v → %q", out.ProviderCalls, r.Severity, findingCodes(r.Findings), deref(r.Translated))
	require.NotEqual(t, translation.Error, r.Severity, r.Err)
	require.Contains(t, *r.Translated, "`OP_CALL`")
}
