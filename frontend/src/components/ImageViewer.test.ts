import { mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { nextTick } from 'vue'

import ImageViewer from './ImageViewer.vue'

// Просмотрщик открывается поверх страницы (NModal, телепорт в body):
// элементы ищутся в документе, события — настоящие DOM-события.

let w: VueWrapper | null = null

afterEach(() => {
  w?.unmount()
  w = null
})

async function open() {
  w = mount(ImageViewer, { props: { src: '/a.png', alt: 'Схема разбора' }, attachTo: document.body })
  await nextTick()
  const img = document.querySelector<HTMLImageElement>('.image-viewer img')!
  const stage = document.querySelector<HTMLElement>('.image-viewer .stage')!
  // В jsdom раскладки нет: размеры вписанной картинки и сцены — как на экране.
  Object.defineProperties(img, { offsetWidth: { value: 400 }, offsetHeight: { value: 300 } })
  Object.defineProperties(stage, { clientWidth: { value: 500 }, clientHeight: { value: 500 } })
  return { img, stage }
}

/** Масштаб и сдвиг из transform картинки. */
function view(img: HTMLImageElement) {
  const m = /translate\((-?[\d.]+)px, (-?[\d.]+)px\) scale\(([\d.]+)\)/.exec(img.style.transform)
  expect(m, img.style.transform).not.toBeNull()
  return { x: Number(m![1]), y: Number(m![2]), scale: Number(m![3]) }
}

function button(name: string) {
  const b = [...document.querySelectorAll<HTMLButtonElement>('.image-viewer button')].find(
    (el) => el.getAttribute('aria-label') === name || el.textContent?.trim() === name,
  )
  expect(b, name).toBeDefined()
  return b!
}

function pointer(type: string, target: EventTarget, id: number, x: number, y: number) {
  target.dispatchEvent(new PointerEvent(type, { pointerId: id, clientX: x, clientY: y, bubbles: true }))
}

describe('просмотр картинки', () => {
  it('открывается диалогом с подписью картинки', async () => {
    const { img } = await open()
    const dialog = document.querySelector('.image-viewer')!
    expect(dialog.getAttribute('role')).toBe('dialog')
    expect(dialog.getAttribute('aria-label')).toBe('Схема разбора')
    expect(img.getAttribute('src')).toBe('/a.png')
    expect(view(img)).toEqual({ x: 0, y: 0, scale: 1 })
  })

  it('кнопки увеличивают, уменьшают и вписывают', async () => {
    const { img } = await open()
    button('Увеличить').click()
    await nextTick()
    expect(view(img).scale).toBe(1.5)
    expect(document.querySelector('.image-viewer .zoom-level')?.textContent).toBe('150%')
    button('Увеличить').click()
    await nextTick()
    expect(view(img).scale).toBe(2.25)
    button('Уменьшить').click()
    await nextTick()
    expect(view(img).scale).toBe(1.5)
    button('Вписать').click()
    await nextTick()
    expect(view(img)).toEqual({ x: 0, y: 0, scale: 1 })
    expect(button('Вписать').disabled).toBe(true)
  })

  it('колесо мыши увеличивает у курсора', async () => {
    const { img, stage } = await open()
    // Центр сцены — (250, 250): курсор правее центра, картинка уходит влево.
    stage.dispatchEvent(new WheelEvent('wheel', { deltaY: -200, clientX: 400, clientY: 250, bubbles: true, cancelable: true }))
    await nextTick()
    const v = view(img)
    expect(v.scale).toBeGreaterThan(1)
    expect(v.x).toBeLessThan(0)
    expect(v.y).toBe(0)
  })

  it('перетаскивание двигает увеличенную картинку', async () => {
    const { img } = await open()
    button('Увеличить').click()
    button('Увеличить').click()
    await nextTick()
    pointer('pointerdown', img, 1, 200, 200)
    pointer('pointermove', window, 1, 230, 180)
    pointer('pointerup', window, 1, 230, 180)
    await nextTick()
    expect(view(img)).toEqual({ x: 30, y: -20, scale: 2.25 })
  })

  it('два пальца разводят — картинка растёт', async () => {
    const { img } = await open()
    pointer('pointerdown', img, 1, 100, 100)
    pointer('pointerdown', img, 2, 200, 100)
    pointer('pointermove', window, 2, 300, 100)
    pointer('pointerup', window, 1, 100, 100)
    pointer('pointerup', window, 2, 300, 100)
    await nextTick()
    expect(view(img).scale).toBe(2)
  })

  it('двойное касание увеличивает и возвращает обратно', async () => {
    const { img } = await open()
    for (let i = 0; i < 2; i++) {
      pointer('pointerdown', img, 1, 0, 0)
      pointer('pointerup', window, 1, 0, 0)
    }
    await nextTick()
    expect(view(img).scale).toBe(2.5)
    for (let i = 0; i < 2; i++) {
      pointer('pointerdown', img, 1, 0, 0)
      pointer('pointerup', window, 1, 0, 0)
    }
    await nextTick()
    expect(view(img).scale).toBe(1)
  })

  it('клавиши: плюс, минус и ноль', async () => {
    const { img } = await open()
    const dialog = document.querySelector('.image-viewer')!
    dialog.dispatchEvent(new KeyboardEvent('keydown', { key: '+', bubbles: true }))
    await nextTick()
    expect(view(img).scale).toBe(1.5)
    dialog.dispatchEvent(new KeyboardEvent('keydown', { key: '0', bubbles: true }))
    await nextTick()
    expect(view(img).scale).toBe(1)
  })

  it('закрывается кнопкой и касанием мимо картинки, но не после перетаскивания', async () => {
    const { stage } = await open()
    pointer('pointerdown', stage, 1, 10, 10)
    pointer('pointermove', window, 1, 80, 10)
    pointer('pointerup', window, 1, 80, 10)
    stage.click()
    expect(w!.emitted('close')).toBeUndefined()

    stage.click()
    expect(w!.emitted('close')).toHaveLength(1)
    button('Закрыть').click()
    expect(w!.emitted('close')).toHaveLength(2)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', code: 'Escape', bubbles: true }))
    await nextTick()
    expect(w!.emitted('close')).toHaveLength(3)
  })
})
