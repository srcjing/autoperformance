import 'vue-router'

// 扩展路由 meta 类型（对齐 frontend-structure.md §6）：
// title 驱动面包屑/侧边栏；module + permission 驱动权限守卫；public 标记公开路由。
declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    public?: boolean
    requiresAuth?: boolean
    module?: string
    permission?: 'read' | 'write'
  }
}
