// Подмена EventSource: тест сам посылает события и ошибки потока.
import { vi } from 'vitest'

export class FakeEventSource {
  static instances: FakeEventSource[] = []
  url: string
  closed = false
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null

  constructor(url: string) {
    this.url = url
    FakeEventSource.instances.push(this)
  }

  close() {
    this.closed = true
  }

  emit(data: unknown) {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(data) }))
  }

  fail() {
    this.onerror?.(new Event('error'))
  }
}

export function stubEventSource() {
  FakeEventSource.instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
  return FakeEventSource.instances
}
