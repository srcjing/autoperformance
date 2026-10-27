import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import { setHttpHooks } from './api/http'
import { permissionDirective } from './directives/permission'
import { useAuthStore } from './stores/auth'
import './assets/main.css'

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
app.use(router)
// 按钮级权限指令（v-permission）：前端隐藏不可替代服务端鉴权。
app.directive('permission', permissionDirective)

// 全局 HTTP 拦截回调：401/10102 清理会话回登录、10103 强制改密、403 跳无权限页。
const auth = useAuthStore(pinia)
const currentRouteName = (): unknown => router.currentRoute.value.name

setHttpHooks({
  onUnauthorized: () => {
    auth.clearSession()
    if (currentRouteName() !== 'login') {
      void router.replace({
        name: 'login',
        query: { redirect: router.currentRoute.value.fullPath },
      })
    }
  },
  onForbidden: () => {
    if (currentRouteName() !== 'forbidden') {
      void router.replace({ name: 'forbidden' })
    }
  },
  onMustChangePassword: () => {
    if (currentRouteName() !== 'change-password') {
      void router.replace({ name: 'change-password' })
    }
  },
})

app.mount('#app')
