export const APP_PREFIX = '/ai/open/cad'

export function apiPath(endpoint: string): string {
  return joinPath(appBasePath(), endpoint)
}

export function assetPath(path: string): string {
  return joinPath(appBasePath(), path)
}

function appBasePath(): string {
  const pathname = window.location.pathname
  const index = pathname.indexOf(APP_PREFIX)
  if (index >= 0) {
    return pathname.slice(0, index + APP_PREFIX.length)
  }

  const viteBase = import.meta.env.BASE_URL
  if (viteBase && viteBase !== '/' && viteBase !== './') {
    return viteBase.replace(/\/$/, '')
  }
  return ''
}

function joinPath(base: string, path: string): string {
  const cleanBase = base.replace(/\/$/, '')
  const cleanPath = path.replace(/^\//, '')
  return cleanBase ? `${cleanBase}/${cleanPath}` : `/${cleanPath}`
}
