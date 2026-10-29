import { onUnmounted, ref, type Ref } from 'vue'
import { getToken } from '@/api/http'
import type { WsEnvelope } from '@/types'

// 实时通道（api-contract §2.4 / frontend-structure §3.1）。
//
// 协议：
//   - 握手：`WS /api/v1/ws?token=<jwt>`（token 经 query 传递，服务端校验签名/有效期/权限并集）；
//   - 指令：`{"action":"subscribe"|"unsubscribe"|"ping","channel":"task:12"}`；
//   - 推送：`{channel,event,data,time}`，事件名 `status` / `log` / `metrics` / `evaluation`；
//   - 频道：`task:{id}`（需 testing.task:read）、`logs:{type}`（需 log.{type}:read）。
//
// 可靠性：断线指数退避重连（上限 maxReconnectDelay）、重连后自动补订阅、应用层心跳 +
// 死连接探测（>2.5 个心跳周期无任何消息视为假连接）。组件卸载时自动关闭（useWebSocket）。

export type WsEventHandler<T = unknown> = (data: T, envelope: WsEnvelope<T>) => void

export interface WebSocketOptions {
  /** 是否自动重连（默认 true）。 */
  autoReconnect?: boolean
  /** 重连退避上限（ms，默认 15000）。 */
  maxReconnectDelay?: number
  /** 应用层心跳间隔（ms，默认 25000；服务端 pongWait=60s）。 */
  heartbeatInterval?: number
}

export interface WebSocketClient {
  readonly connected: Ref<boolean>
  readonly lastError: Ref<string>
  /** 当前订阅频道（只读快照）。 */
  readonly channels: Ref<string[]>
  connect: () => void
  close: () => void
  subscribe: (channel: string) => void
  unsubscribe: (channel: string) => void
  /** 注册事件处理器（`*` 表示全部事件），返回取消订阅函数。 */
  on: <T = unknown>(event: string, handler: WsEventHandler<T>) => () => void
}

const WS_PATH = '/api/v1/ws'
const DEFAULT_HEARTBEAT = 25000
const DEFAULT_MAX_DELAY = 15000
const MAX_BACKOFF_STEPS = 5

/** 组装握手地址（同源部署，协议随页面自动切换 ws/wss）。 */
function buildUrl(): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const token = getToken()
  return `${proto}//${window.location.host}${WS_PATH}?token=${encodeURIComponent(token)}`
}

/** 创建一条实时通道客户端（生命周期由调用方管理；Store 用它做长连接）。 */
export function createWebSocket(options: WebSocketOptions = {}): WebSocketClient {
  const autoReconnect = options.autoReconnect ?? true
  const maxReconnectDelay = options.maxReconnectDelay ?? DEFAULT_MAX_DELAY
  const heartbeatInterval = options.heartbeatInterval ?? DEFAULT_HEARTBEAT

  const connected = ref(false)
  const lastError = ref('')
  const channels = ref<string[]>([])

  // 事件名 → 处理器集合（`*` 为通配）。
  const handlers = new Map<string, Set<(data: unknown, envelope: WsEnvelope) => void>>()
  let socket: WebSocket | null = null
  let closedByUser = false
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null
  let heartbeatTimer: ReturnType<typeof setInterval> | null = null
  let attempts = 0
  let lastMessageAt = 0

  /** 分发事件到对应处理器（单个处理器异常不影响其他订阅）。 */
  function dispatch(envelope: WsEnvelope): void {
    for (const key of [envelope.event, '*']) {
      const set = handlers.get(key)
      if (!set) continue
      for (const handler of Array.from(set)) {
        try {
          handler(envelope.data, envelope)
        } catch {
          // 忽略单个订阅抛出的异常，避免污染实时通道
        }
      }
    }
  }

  function send(payload: unknown): void {
    if (!socket || socket.readyState !== WebSocket.OPEN) return
    try {
      socket.send(JSON.stringify(payload))
    } catch {
      // 发送失败由 onclose 统一重连
    }
  }

  function stopHeartbeat(): void {
    if (heartbeatTimer !== null) {
      clearInterval(heartbeatTimer)
      heartbeatTimer = null
    }
  }

  function startHeartbeat(): void {
    stopHeartbeat()
    lastMessageAt = Date.now()
    heartbeatTimer = setInterval(() => {
      if (!socket || socket.readyState !== WebSocket.OPEN) return
      // 长时间无任何消息（含订阅回执/推送）视为假连接，主动断开触发重连。
      if (Date.now() - lastMessageAt > heartbeatInterval * 2.5) {
        socket.close()
        return
      }
      send({ action: 'ping' })
    }, heartbeatInterval)
  }

  function scheduleReconnect(): void {
    if (!autoReconnect || closedByUser || reconnectTimer !== null) return
    const step = Math.min(attempts, MAX_BACKOFF_STEPS)
    const base = Math.min(1000 * 2 ** step, maxReconnectDelay)
    attempts += 1
    reconnectTimer = setTimeout(
      () => {
        reconnectTimer = null
        connect()
      },
      base + Math.floor(Math.random() * 500),
    )
  }

  function connect(): void {
    if (typeof WebSocket === 'undefined') {
      lastError.value = '当前浏览器不支持 WebSocket'
      return
    }
    if (!getToken()) {
      lastError.value = '未登录，无法建立实时通道'
      return
    }
    if (
      socket &&
      (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)
    ) {
      return
    }

    closedByUser = false
    let next: WebSocket
    try {
      next = new WebSocket(buildUrl())
    } catch {
      lastError.value = '实时通道创建失败'
      scheduleReconnect()
      return
    }
    socket = next

    next.onopen = () => {
      connected.value = true
      lastError.value = ''
      attempts = 0
      // 重连后补订阅（服务端订阅状态不跨连接保留）。
      for (const channel of channels.value) send({ action: 'subscribe', channel })
      startHeartbeat()
    }

    next.onmessage = (event: MessageEvent) => {
      lastMessageAt = Date.now()
      let envelope: WsEnvelope
      try {
        envelope = JSON.parse(String(event.data)) as WsEnvelope
      } catch {
        return
      }
      if (envelope.event === 'error') {
        const data = envelope.data as { message?: string } | undefined
        lastError.value = data?.message ?? '实时通道指令失败'
      }
      dispatch(envelope)
    }

    next.onerror = () => {
      // 浏览器不暴露握手失败细节，统一提示；重连由 onclose 触发。
      lastError.value = '实时通道异常'
    }

    next.onclose = () => {
      connected.value = false
      stopHeartbeat()
      socket = null
      if (!closedByUser) scheduleReconnect()
    }
  }

  function close(): void {
    closedByUser = true
    if (reconnectTimer !== null) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    stopHeartbeat()
    const current = socket
    socket = null
    connected.value = false
    if (current) {
      current.onopen = null
      current.onmessage = null
      current.onerror = null
      current.onclose = null
      try {
        current.close()
      } catch {
        // 已关闭，忽略
      }
    }
  }

  function subscribe(channel: string): void {
    if (!channel) return
    if (!channels.value.includes(channel)) {
      channels.value = [...channels.value, channel]
    }
    if (socket && socket.readyState === WebSocket.OPEN) {
      send({ action: 'subscribe', channel })
      return
    }
    connect()
  }

  function unsubscribe(channel: string): void {
    channels.value = channels.value.filter((item) => item !== channel)
    if (socket && socket.readyState === WebSocket.OPEN) {
      send({ action: 'unsubscribe', channel })
    }
  }

  function on<T = unknown>(event: string, handler: WsEventHandler<T>): () => void {
    const wrapped = (data: unknown, envelope: WsEnvelope): void => {
      handler(data as T, envelope as WsEnvelope<T>)
    }
    const set = handlers.get(event) ?? new Set<typeof wrapped>()
    set.add(wrapped)
    handlers.set(event, set)
    return () => {
      set.delete(wrapped)
      if (set.size === 0) handlers.delete(event)
    }
  }

  return {
    connected,
    lastError,
    channels,
    connect,
    close,
    subscribe,
    unsubscribe,
    on,
  }
}

/**
 * 组件内使用实时通道：返回客户端并自动连接首个订阅频道。
 *
 * 注意：必须在 setup 内调用（依赖 onUnmounted 关闭连接）。
 */
export function useWebSocket(options: WebSocketOptions = {}): WebSocketClient {
  const client = createWebSocket(options)
  onUnmounted(() => client.close())
  return client
}
