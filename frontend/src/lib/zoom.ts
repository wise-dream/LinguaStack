// Масштаб и сдвиг картинки в просмотрщике. Картинка стоит в центре сцены
// вписанной (scale 1) и смещается на x, y; точки считаются от центра сцены.

export interface View {
  scale: number
  x: number
  y: number
}

export interface Size {
  w: number
  h: number
}

export const MAX_SCALE = 8

export const fitView: View = { scale: 1, x: 0, y: 0 }

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v))

/** Новый масштаб вокруг точки (px, py): точка под курсором или пальцами остаётся на месте. */
export function zoomAt(v: View, next: number, px: number, py: number): View {
  const scale = clamp(next, 1, MAX_SCALE)
  const k = scale / v.scale
  return { scale, x: px - (px - v.x) * k, y: py - (py - v.y) * k }
}

/**
 * Сдвиг без ухода картинки со сцены: увеличенная двигается, пока её край
 * не дошёл до края сцены; меньшая сцены по этой оси стоит по центру.
 * fit — размер вписанной картинки, stage — размер сцены.
 */
export function clampPan(v: View, fit: Size, stage: Size): View {
  const limit = (len: number, room: number) => Math.max(0, (len * v.scale - room) / 2)
  const lx = limit(fit.w, stage.w)
  const ly = limit(fit.h, stage.h)
  // + 0 превращает -0 в 0: иначе toEqual отличает их, а смысл один.
  return { scale: v.scale, x: clamp(v.x, -lx, lx) + 0, y: clamp(v.y, -ly, ly) + 0 }
}
