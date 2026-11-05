import { createRouter, createWebHistory } from 'vue-router'
import { adminToken } from './api'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'print', component: () => import('./views/PrintView.vue') },
    { path: '/scan', name: 'scan', component: () => import('./views/ScanView.vue') },
    { path: '/admin/login', name: 'admin-login', component: () => import('./views/AdminLogin.vue') },
    {
      path: '/admin',
      component: () => import('./views/AdminLayout.vue'),
      children: [
        { path: '', name: 'admin-dashboard', component: () => import('./views/DashboardView.vue') },
        { path: 'printers', name: 'admin-printers', component: () => import('./views/PrintersView.vue') },
        { path: 'sharing', name: 'admin-sharing', component: () => import('./views/SharingView.vue') },
        { path: 'drivers', name: 'admin-drivers', component: () => import('./views/DriversView.vue') },
        { path: 'cups', name: 'admin-cups', component: () => import('./views/CupsView.vue') },
        { path: 'jobs', name: 'admin-jobs', component: () => import('./views/JobsView.vue') },
        { path: 'settings', name: 'admin-settings', component: () => import('./views/SettingsView.vue') },
        { path: 'logs', name: 'admin-logs', component: () => import('./views/LogsView.vue') },
      ],
    },
  ],
})

router.beforeEach((to) => {
  if (to.path.startsWith('/admin') && to.path !== '/admin/login' && !adminToken.value) {
    return { path: '/admin/login', query: { redirect: to.fullPath } }
  }
  return true
})

export default router
