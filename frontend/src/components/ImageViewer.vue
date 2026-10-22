<script setup lang="ts">
// Просмотр картинки книги поверх страницы: масштаб — колесом, щипком,
// двойным касанием, кнопками и клавишами (+, −, 0); увеличенную картинку
// двигают мышью, пальцем или стрелками. Со сцены она не уходит (lib/zoom).
import { NButton, NModal } from 'naive-ui'
import { computed, onBeforeUnmount, ref } from 'vue'

import { MAX_SCALE, clampPan, fitView, zoomAt, type View } from '../lib/zoom'

defineProps<{ src: string; alt: string }>()
const emit = defineEmits<{ close: [] }>()

/** Шаг кнопок и клавиш; двойное касание увеличивает сразу до TAP_SCALE. */
const STEP = 1.5
const TAP_SCALE = 2.5
const ARROW_PAN = 60

const stage = ref<HTMLElement | null>(null)
const img = ref<HTMLImageElement | null>(null)
const view = ref<View>(fitView)
// Кнопки и клавиши меняют масштаб плавно; за пальцем и колесом картинка
// идёт сразу, иначе отстаёт.
const smooth = ref(false)
const dragging = ref(false)

const transform = computed(() => `translate(${view.value.x}px, ${view.value.y}px) scale(${view.value.scale})`)
const percent = computed(() => `${Math.round(view.value.scale * 100)}%`)
const atFit = computed(() => view.value.scale === 1 && view.value.x === 0 && view.value.y === 0)

function apply(v: View, animate: boolean) {
  smooth.value = animate
  const s = stage.value
  const i = img.value
  view.value = s && i ? clampPan(v, { w: i.offsetWidth, h: i.offsetHeight }, { w: s.clientWidth, h: s.clientHeight }) : v
}

/** Точка экрана в координатах от центра сцены. */
function local(x: number, y: number) {
  const s = stage.value
  if (!s) return { x: 0, y: 0 }
  const r = s.getBoundingClientRect()
  return { x: x - r.left - s.clientWidth / 2, y: y - r.top - s.clientHeight / 2 }
}

function zoomBy(k: number) {
  apply(zoomAt(view.value, view.value.scale * k, 0, 0), true)
}

function fit() {
  apply(fitView, true)
}

function onWheel(e: WheelEvent) {
  // Firefox шлёт колесо строками, а не пикселями; щипок на тачпаде приходит
  // колесом с Ctrl и мелким шагом.
  const dy = e.deltaMode === 1 ? e.deltaY * 16 : e.deltaY
  const c = local(e.clientX, e.clientY)
  apply(zoomAt(view.value, view.value.scale * Math.exp(-dy * (e.ctrlKey ? 0.01 : 0.002)), c.x, c.y), false)
}

// Жест — один или два указателя (мышь, палец, перо). Пока он идёт,
// движение слушается на окне: палец может уйти за край сцены.
const pointers = new Map<number, { x: number; y: number }>()
let gesture: { view: View; x: number; y: number; dist: number } | null = null
let moved = false
let fromImage = false
let lastTap: { t: number; x: number; y: number } | null = null
// Перетаскивание кончается щелчком по сцене — он не должен закрыть просмотр.
let swallowClick = false

function points() {
  return [...pointers.values()]
}

function begin() {
  const pts = points()
  const mid = { x: pts.reduce((a, p) => a + p.x, 0) / pts.length, y: pts.reduce((a, p) => a + p.y, 0) / pts.length }
  const dist = pts.length > 1 ? Math.hypot(pts[0]!.x - pts[1]!.x, pts[0]!.y - pts[1]!.y) : 0
  gesture = { view: view.value, ...mid, dist }
}

function listen(on: boolean) {
  if (on) {
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    window.addEventListener('pointercancel', onUp)
  } else {
    window.removeEventListener('pointermove', onMove)
    window.removeEventListener('pointerup', onUp)
    window.removeEventListener('pointercancel', onUp)
  }
}

function onDown(e: PointerEvent) {
  if (e.button > 0 || pointers.size >= 2) return
  if (pointers.size === 0) {
    moved = false
    swallowClick = false
    fromImage = e.target === img.value
    listen(true)
  }
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY })
  begin()
}

function onMove(e: PointerEvent) {
  const p = pointers.get(e.pointerId)
  if (!p || !gesture) return
  p.x = e.clientX
  p.y = e.clientY
  const pts = points()
  const mx = pts.reduce((a, q) => a + q.x, 0) / pts.length
  const my = pts.reduce((a, q) => a + q.y, 0) / pts.length
  let v = gesture.view
  if (pts.length > 1 && gesture.dist > 0) {
    const c = local(gesture.x, gesture.y)
    const dist = Math.hypot(pts[0]!.x - pts[1]!.x, pts[0]!.y - pts[1]!.y)
    v = zoomAt(v, v.scale * (dist / gesture.dist), c.x, c.y)
    moved = true
  }
  if (Math.hypot(mx - gesture.x, my - gesture.y) > 4) moved = true
  if (!moved) return
  dragging.value = true
  apply({ ...v, x: v.x + mx - gesture.x, y: v.y + my - gesture.y }, false)
}

function onUp(e: PointerEvent) {
  if (!pointers.delete(e.pointerId)) return
  if (pointers.size > 0) {
    // Один палец из двух поднят — второй продолжает двигать с того же места.
    begin()
    return
  }
  listen(false)
  gesture = null
  dragging.value = false
  if (moved) {
    swallowClick = true
    lastTap = null
    return
  }
  if (!fromImage) return
  const now = performance.now()
  if (lastTap && now - lastTap.t < 300 && Math.hypot(e.clientX - lastTap.x, e.clientY - lastTap.y) < 30) {
    const c = local(e.clientX, e.clientY)
    apply(view.value.scale > 1 ? fitView : zoomAt(view.value, TAP_SCALE, c.x, c.y), true)
    lastTap = null
  } else {
    lastTap = { t: now, x: e.clientX, y: e.clientY }
  }
}

function onStageClick(e: MouseEvent) {
  if (swallowClick) {
    swallowClick = false
    return
  }
  // Касание мимо картинки закрывает просмотр, как щелчок по затемнению.
  if (e.target === e.currentTarget) emit('close')
}

function onKey(e: KeyboardEvent) {
  const v = view.value
  switch (e.key) {
    case '+':
    case '=':
      zoomBy(STEP)
      break
    case '-':
    case '_':
      zoomBy(1 / STEP)
      break
    case '0':
      fit()
      break
    // Стрелка открывает ту часть картинки, куда указывает, — как прокрутка.
    case 'ArrowLeft':
      apply({ ...v, x: v.x + ARROW_PAN }, true)
      break
    case 'ArrowRight':
      apply({ ...v, x: v.x - ARROW_PAN }, true)
      break
    case 'ArrowUp':
      apply({ ...v, y: v.y + ARROW_PAN }, true)
      break
    case 'ArrowDown':
      apply({ ...v, y: v.y - ARROW_PAN }, true)
      break
    default:
      return
  }
  e.preventDefault()
}

onBeforeUnmount(() => listen(false))
</script>

<template>
  <NModal
    :show="true"
    @update:show="emit('close')"
  >
    <div
      class="image-viewer"
      role="dialog"
      aria-modal="true"
      :aria-label="alt || 'Картинка'"
      @keydown="onKey"
    >
      <div class="corner">
        <NButton
          quaternary
          circle
          aria-label="Закрыть"
          @click="emit('close')"
        >
          <svg
            viewBox="0 0 24 24"
            width="20"
            height="20"
            aria-hidden="true"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            stroke-linecap="round"
          ><path d="M6 6l12 12M18 6L6 18" /></svg>
        </NButton>
      </div>
      <div
        ref="stage"
        class="stage"
        :class="{ zoomed: view.scale > 1, dragging }"
        @pointerdown="onDown"
        @wheel.prevent="onWheel"
        @click="onStageClick"
      >
        <img
          ref="img"
          :src="src"
          :alt="alt"
          :class="{ smooth }"
          :style="{ transform }"
          draggable="false"
        >
      </div>
      <div
        class="toolbar"
        role="toolbar"
        aria-label="Масштаб"
      >
        <NButton
          quaternary
          circle
          aria-label="Уменьшить"
          :disabled="view.scale <= 1"
          @click="zoomBy(1 / STEP)"
        >
          <svg
            viewBox="0 0 24 24"
            width="20"
            height="20"
            aria-hidden="true"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            stroke-linecap="round"
          ><path d="M5 12h14" /></svg>
        </NButton>
        <span class="zoom-level">{{ percent }}</span>
        <NButton
          quaternary
          circle
          aria-label="Увеличить"
          :disabled="view.scale >= MAX_SCALE"
          @click="zoomBy(STEP)"
        >
          <svg
            viewBox="0 0 24 24"
            width="20"
            height="20"
            aria-hidden="true"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            stroke-linecap="round"
          ><path d="M5 12h14M12 5v14" /></svg>
        </NButton>
        <NButton
          quaternary
          round
          :disabled="atFit"
          @click="fit"
        >
          Вписать
        </NButton>
      </div>
    </div>
  </NModal>
</template>

<style scoped>
/* Окно модалки Naive — во весь экран: модалка сама держит его поверх
   страницы, запирает прокрутку и фокус. */
.image-viewer {
  width: 100%;
  height: 100vh;
  height: 100dvh;
  background: var(--paper);
}

/* Сцена — весь экран: картинка вписана с полями под кнопки. Браузер не
   перехватывает жесты — щипок и сдвиг принадлежат просмотру. */
.stage {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--space-7) var(--space-4) calc(var(--space-7) + var(--space-5));
  overflow: hidden;
  touch-action: none;
  overscroll-behavior: contain;
  user-select: none;
}

.stage img {
  min-width: 0;
  min-height: 0;
  max-width: 100%;
  max-height: 100%;
  cursor: zoom-in;
}

.stage.zoomed img {
  cursor: grab;
}

.stage.dragging img {
  cursor: grabbing;
}

.stage img.smooth {
  transition: transform 0.2s ease-out;
}

.corner,
.toolbar {
  position: absolute;
  z-index: 1;
  display: flex;
  align-items: center;
  gap: var(--space-1);
  padding: var(--space-1);
  background: var(--surface);
  border: 1px solid var(--rule-soft);
  border-radius: 999px;
  box-shadow: var(--shadow-pop);
}

.corner {
  top: var(--space-3);
  right: var(--space-3);
}

.toolbar {
  bottom: max(var(--space-4), env(safe-area-inset-bottom));
  left: 50%;
  transform: translateX(-50%);
}

.zoom-level {
  min-width: 3.5em;
  text-align: center;
  font-variant-numeric: tabular-nums;
  color: var(--muted);
  font-size: var(--step--1);
}

@media (prefers-reduced-motion: reduce) {
  .stage img.smooth {
    transition: none;
  }
}
</style>
