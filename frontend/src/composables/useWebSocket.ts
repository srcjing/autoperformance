import { ref, onUnmounted, type Ref } from 'vue'

// useWebSocket 占位：后续接入 WS /api/v1/ws?token=...，订阅 task:{id} 与 logs:{type} 频道。
// 对齐 frontend-structure.md §3.1：实时数据由本 composable 订阅，事件回调更新对应 Store。
export function useWebSocket(_url: string): { connected: Ref<boolean> } {
  const connected = ref(false)

  // 骨架：未建立真实连接，仅占位。
  onUnmounted(() => {
    connected.value = false
  })

  return { connected }
}
