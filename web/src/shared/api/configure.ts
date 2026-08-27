import { OpenAPI } from '@/api/generated'

let csrfToken: string | undefined

export function setCsrfToken(token?: string) {
  csrfToken = token
}

export function getCsrfToken() {
  return csrfToken
}

export function configureApi() {
  OpenAPI.WITH_CREDENTIALS = true
  OpenAPI.CREDENTIALS = 'include'
  OpenAPI.HEADERS = async (): Promise<Record<string, string>> => csrfToken ? { 'X-CSRF-Token': csrfToken } : {}
}
