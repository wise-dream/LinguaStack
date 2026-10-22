package translation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/vnovels/linguastack/internal/cdm"
	"github.com/vnovels/linguastack/internal/providers"
)

// Service переводит батч: маскирование → конверт → модель → разбор →
// восстановление → проверка. Деградация при сбоях протокола — по ступеням
// дополнения §3 (отказ, обрыв по лимиту токенов и тайм-аут на батче сразу
// ведут на ступень 2):
//
//  1. повтор батча с усиленным напоминанием о формате;
//  2. по одному сегменту — медленнее, но почти не ломается;
//     2а. сегмент потерял метки, а в нём есть инлайн-код — повтор с кодом
//     на виду (codeInSight);
//  3. сегмент — ошибка с сохранённым последним ответом модели; книга
//     продолжает переводиться.
//
// Ошибки протокола и качества сервис обрабатывает сам. Инфраструктурные
// (недоступность, лимит частоты, неверный ключ) возвращает наружу: паузами
// и повторами управляет очередь. Вместе с такой ошибкой возвращаются уже
// готовые результаты — они оплачены и не должны теряться (Review Focus №5).
type Service struct {
	provider providers.Provider
	modelID  string
	opts     ServiceOptions
}

// ServiceOptions — бюджет повторов.
type ServiceOptions struct {
	BatchRetries      int // повторы батча на ступени 1; по умолчанию 2
	SingletonAttempts int // попытки одного сегмента на ступени 2; по умолчанию 2
}

// SegmentOutcome — итог по сегменту. Translated == nil — перевода нет:
// текст с потерянным кодом не должен уйти в экспорт как перевод.
type SegmentOutcome struct {
	NodeID     string
	Translated *string
	Severity   Severity
	Findings   []Finding
	Err        string
}

// BatchOutcome — итог батча. Сегменты, до которых дело не дошло из-за
// сбоя инфраструктуры, в Results отсутствуют.
type BatchOutcome struct {
	Results       map[string]SegmentOutcome
	ProviderCalls int
	CostUSD       float64
	// Токены — по всем вызовам батча, если провайдер их сообщает.
	InputTokens  int64
	OutputTokens int64
}

const maxRawInError = 4000

const formatReminder = "IMPORTANT: your previous reply did not follow the required format. " +
	"Reply only with the numbered marker lines, each followed by its translation, and the final END marker — nothing else.\n\n"

// NewService создаёт сервис для модели провайдера.
func NewService(p providers.Provider, modelID string, opts ServiceOptions) *Service {
	if opts.BatchRetries <= 0 {
		opts.BatchRetries = 2
	}
	if opts.SingletonAttempts <= 0 {
		opts.SingletonAttempts = 2
	}
	return &Service{provider: p, modelID: modelID, opts: opts}
}

type batchJob struct {
	svc    *Service
	out    *BatchOutcome
	system string
	det    providers.Determinism
	src    string
	tgt    string
}

// TranslateBatch переводит батч с деградацией по ступеням §3.
func (s *Service) TranslateBatch(ctx context.Context, b Batch, system string, det providers.Determinism, src, tgt string) (BatchOutcome, error) {
	out := BatchOutcome{Results: make(map[string]SegmentOutcome, len(b.Segments))}
	job := &batchJob{svc: s, out: &out, system: system, det: det, src: src, tgt: tgt}

	retry, err := job.attempt(ctx, b, s.opts.BatchRetries+1, cdm.Mask)
	if err != nil {
		return out, err
	}
	if len(b.Segments) == 1 {
		return out, job.codeInSight(ctx, b)
	}
	for _, i := range retry {
		single := Batch{Segments: []SegmentRef{b.Segments[i]}, ContextBefore: b.ContextBefore, ContextAfter: b.ContextAfter}
		if i > 0 {
			single.ContextBefore = tail(b.Segments[i-1].Text, contextChars)
		}
		if i+1 < len(b.Segments) {
			single.ContextAfter = head(b.Segments[i+1].Text, contextChars)
		}
		if _, err := job.attempt(ctx, single, s.opts.SingletonAttempts, cdm.Mask); err != nil {
			return out, err
		}
		if err := job.codeInSight(ctx, single); err != nil {
			return out, err
		}
	}
	return out, nil
}

// codeInSight — последняя ступень для одного сегмента (дополнение §3): он
// не прошёл проверку из-за потерянных меток, а в нём есть инлайн-код, —
// ещё попытки с кодом на виду (cdm.MaskKeepingCode). Метку на месте кода
// слабая модель выбрасывает — для неё это пустое место в предложении, — а
// сам код переносит; сохранность кода сверяет ValidateSegment.
func (j *batchJob) codeInSight(ctx context.Context, b Batch) error {
	seg := b.Segments[0]
	r := j.out.Results[seg.NodeID]
	lostMarkers := slices.ContainsFunc(r.Findings, func(f Finding) bool { return f.Code == "placeholder_mismatch" })
	if r.Translated != nil || !lostMarkers || len(cdm.InlineCode(cdm.MaskKeepingCode(seg.Text).Text)) == 0 {
		return nil
	}
	_, err := j.attempt(ctx, b, j.svc.opts.SingletonAttempts, cdm.MaskKeepingCode)
	return err
}

// attempt делает до attempts запросов для батча; mask закрывает метками
// защищённые фрагменты. Для батча из нескольких сегментов возвращает
// индексы тех, что надо перевести поодиночке; для одного сегмента всегда
// записывает итог сам.
func (j *batchJob) attempt(ctx context.Context, b Batch, attempts int, mask func(string) cdm.Masked) (retry []int, err error) {
	n := len(b.Segments)
	masks := make([]cdm.Masked, n)
	masked := Batch{Segments: make([]SegmentRef, n), ContextBefore: b.ContextBefore, ContextAfter: b.ContextAfter}
	for i, seg := range b.Segments {
		masks[i] = ProtectEnvelope(mask(seg.Text))
		masked.Segments[i] = seg
		masked.Segments[i].Text = masks[i].Text
	}
	all := func() []int {
		idx := make([]int, n)
		for i := range idx {
			idx[i] = i
		}
		return idx
	}

	lastReason := ""
	var lastFindings []Finding
	// feedback — что именно было не так в прошлом ответе (потерянные
	// метки или код): общее «соблюдай формат» слабая модель пропускает и
	// повторяет ту же ошибку во всех попытках.
	feedback := ""
	for try := 0; try < attempts; try++ {
		prompt := BuildUserPrompt(masked)
		switch {
		case feedback != "":
			prompt = feedback + prompt
		case try > 0:
			prompt = formatReminder + prompt
		}
		res, err := j.svc.provider.Complete(ctx, j.svc.modelID, providers.CompletionRequest{
			System: j.system, User: prompt, Determinism: j.det,
		})
		j.out.ProviderCalls++
		if err != nil {
			var refused *providers.RefusedError
			if errors.As(err, &refused) || errors.Is(err, providers.ErrBadRequest) {
				// Отказ или 400 могут быть вызваны одним сегментом —
				// поодиночке переводятся остальные. Повтор того же текста
				// даст тот же результат.
				if n > 1 {
					return all(), nil
				}
				j.fail(b.Segments[0], nil, "Модель не перевела фрагмент: "+err.Error())
				return nil, nil
			}
			if n > 1 && errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				// Тайм-аут адаптера на батче: модель не успевает его целиком
				// (медленная локальная модель), повтор упал бы так же, а
				// поодиночке ответы короче. Отмена вызывающим (ctx.Err())
				// — остановка, а не повод дробить.
				return all(), nil
			}
			return nil, err
		}
		if res.CostUSD != nil {
			j.out.CostUSD += *res.CostUSD
		}
		if res.InputTokens != nil {
			j.out.InputTokens += int64(*res.InputTokens)
		}
		if res.OutputTokens != nil {
			j.out.OutputTokens += int64(*res.OutputTokens)
		}

		if res.Truncated {
			// Ответ оборван (лимит токенов или зацикливание модели):
			// поодиночке ответ короче, а цикл на другом контексте обычно
			// не повторяется.
			if n > 1 {
				return all(), nil
			}
			lastReason = "Ответ модели оборван: лимит токенов или зацикливание модели.\n" + clip(res.Text)
			continue
		}
		parsed, perr := ParseEnvelope(res.Text, n)
		if perr != nil {
			lastReason = perr.Error() + "\nПоследний ответ модели:\n" + clip(res.Text)
			continue
		}

		var bad []int
		feedback = ""
		for i, seg := range b.Segments {
			// Испорченную метку («⟧2⟧» вместо «⟦2⟧») чиним до проверки:
			// перевод цел, а повтор слабая модель часто портит так же.
			translatedMasked := cdm.RepairMarkers(masks[i].Text, parsed[i+1])
			translated := cdm.Unmask(translatedMasked, masks[i].Placeholders)
			findings := ValidateSegment(ValidationInput{
				Source: seg.Text, Translation: translated,
				MaskedSource: masks[i].Text, MaskedTranslation: translatedMasked,
				SourceLang: j.src, TargetLang: j.tgt, Type: seg.Type, Level: seg.Level,
			})
			if Worst(findings) == Error {
				bad = append(bad, i)
				lastFindings = findings
				lastReason = "Перевод не прошёл проверку.\nПоследний ответ модели:\n" + clip(translatedMasked)
				if n == 1 {
					feedback = retryFeedback(masks[i].Text, translatedMasked)
				}
				continue
			}
			j.out.Results[seg.NodeID] = SegmentOutcome{
				NodeID: seg.NodeID, Translated: &translated, Severity: Worst(findings), Findings: findings,
			}
		}
		if len(bad) == 0 {
			return nil, nil
		}
		if n > 1 {
			return bad, nil
		}
	}

	if n > 1 {
		return all(), nil
	}
	j.fail(b.Segments[0], lastFindings, lastReason)
	return nil, nil
}

// retryFeedback — подсказка для повтора одного сегмента: какие метки
// модель потеряла или выдумала и какой код на виду потеряла или изменила.
// "" — с метками и кодом всё в порядке.
func retryFeedback(masked, translated string) string {
	var mismatch *cdm.PlaceholderMismatchError
	markers := errors.As(cdm.VerifyPlaceholders(masked, translated), &mismatch)
	code := lostCode(masked, translated)
	if !markers && len(code) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("IMPORTANT: your previous translation of this segment was rejected.")
	if markers {
		if len(mismatch.Missing) > 0 {
			b.WriteString(" It lost these protected markers: " + strings.Join(mismatch.Missing, " ") + ".")
		}
		if len(mismatch.Extra) > 0 {
			b.WriteString(" It added markers that are not in the source: " + strings.Join(mismatch.Extra, " ") + ".")
		}
		b.WriteString(" Copy every marker from the source into the translation exactly once and unchanged — ⟦, the number, ⟧ —" +
			" even when you rephrase the sentence. Two markers around words (a link) must stay around their translation.")
	}
	if len(code) > 0 {
		b.WriteString(" It lost or changed this code: " + strings.Join(code, " ") + "." +
			" Copy every `code` span from the source into the translation unchanged, with its backticks.")
	}
	b.WriteString("\n\n")
	return b.String()
}

func (j *batchJob) fail(seg SegmentRef, findings []Finding, reason string) {
	j.out.Results[seg.NodeID] = SegmentOutcome{NodeID: seg.NodeID, Severity: Error, Findings: findings, Err: reason}
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxRawInError {
		return s
	}
	return string([]rune(s)[:maxRawInError]) + fmt.Sprintf("… [обрезано, всего %d символов]", utf8.RuneCountInString(s))
}
