import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const routes = [
  { path: '/', redirect: '/library' },
  { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true } },
  { path: '/library', name: 'library', component: () => import('@/views/LibraryView.vue') },
  { path: '/search', name: 'search', component: () => import('@/views/SearchView.vue') },
  { path: '/reports', name: 'reports', component: () => import('@/views/ReportsView.vue') },
  { path: '/collections', name: 'collections', component: () => import('@/views/CollectionsView.vue') },
  {
    path: '/collections/:id',
    name: 'collection',
    component: () => import('@/views/CollectionDetailView.vue'),
    props: true,
  },
  { path: '/settings', name: 'settings', component: () => import('@/views/SettingsView.vue') },
  { path: '/:pathMatch(.*)*', redirect: '/library' },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()

  // The first navigation happens before the stored token has been checked
  // against /me, so wait for that once rather than flashing the login screen
  // at an already-signed-in user.
  if (!auth.ready) await auth.restore()

  if (!to.meta.public && !auth.isAuthenticated) {
    return { name: 'login', query: to.fullPath === '/library' ? {} : { redirect: to.fullPath } }
  }
  if (to.meta.public && auth.isAuthenticated) {
    return { name: 'library' }
  }
  return true
})

export default router
