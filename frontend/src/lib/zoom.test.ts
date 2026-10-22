import { describe, expect, it } from 'vitest'

import { MAX_SCALE, clampPan, fitView, zoomAt } from './zoom'

describe('масштаб картинки', () => {
  it('точка под курсором остаётся на месте', () => {
    // Курсор в 100 px правее центра сцены: после увеличения вдвое
    // картинка сдвигается влево на те же 100 px — под курсором та же точка.
    const v = zoomAt(fitView, 2, 100, -40)
    expect(v).toEqual({ scale: 2, x: -100, y: 40 })
    const back = zoomAt(v, 1, 100, -40)
    expect(back).toEqual(fitView)
  })

  it('не мельче вписанной и не крупнее предела', () => {
    expect(zoomAt(fitView, 0.3, 0, 0).scale).toBe(1)
    expect(zoomAt(fitView, 100, 0, 0).scale).toBe(MAX_SCALE)
  })
})

describe('сдвиг картинки', () => {
  const fit = { w: 400, h: 300 }
  const stage = { w: 500, h: 500 }

  it('картинка меньше сцены стоит по центру', () => {
    expect(clampPan({ scale: 1, x: 80, y: -60 }, fit, stage)).toEqual(fitView)
  })

  it('край увеличенной картинки не отходит от края сцены', () => {
    // 400×300 вдвое — 800×600: по ширине лишние 300 px, по высоте 100 px —
    // сдвиг не дальше половины в каждую сторону.
    expect(clampPan({ scale: 2, x: 1000, y: -1000 }, fit, stage)).toEqual({ scale: 2, x: 150, y: -50 })
    expect(clampPan({ scale: 2, x: -20, y: 10 }, fit, stage)).toEqual({ scale: 2, x: -20, y: 10 })
  })
})
