import type {
  AsyncJob,
  AsyncJobEvent,
  GenerateCADRequest,
  GenerateCADResponse,
  Project,
  ProjectInput,
  RefineCADRequest,
  RefineCADResponse,
  RepairCADRequest,
  RepairCADResponse,
} from '@/types/api'
import { apiPath } from './paths'

export interface AsyncRequestOptions {
  onEvent?: (event: AsyncJobEvent) => void
}

async function requestJSON<T>(endpoint: string, init?: RequestInit): Promise<T> {
  const url = apiPath(endpoint)
  const clientId = getClientId()
  let response: Response
  try {
    response = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        'X-AI-OpenCAD-Client-ID': clientId,
        ...init?.headers,
      },
    })
  } catch (error) {
    throw new Error(`Request failed before reaching the backend: ${toMessage(error)}`)
  }

  const text = await response.text()
  const payload = parseResponsePayload(text)
  if (!response.ok) {
    throw new Error(responseErrorMessage(url, response, payload, text))
  }
  if (typeof payload === 'string') {
    throw new Error(`Backend returned non-JSON content: ${trimForMessage(payload)}`)
  }
  return payload as T
}

export function generateCAD(
  input: GenerateCADRequest,
  options?: AsyncRequestOptions,
): Promise<GenerateCADResponse> {
  return requestAsyncJob<GenerateCADResponse>(
    'generate-cad-async',
    {
      method: 'POST',
      body: JSON.stringify(input),
    },
    options,
  )
}

export function repairCAD(
  input: RepairCADRequest,
  options?: AsyncRequestOptions,
): Promise<RepairCADResponse> {
  return requestAsyncJob<RepairCADResponse>(
    'repair-cad-async',
    {
      method: 'POST',
      body: JSON.stringify(input),
    },
    options,
  )
}

export function refineCAD(
  input: RefineCADRequest,
  options?: AsyncRequestOptions,
): Promise<RefineCADResponse> {
  return requestAsyncJob<RefineCADResponse>(
    'refine-cad-async',
    {
      method: 'POST',
      body: JSON.stringify(input),
    },
    options,
  )
}

export function listProjects(): Promise<Project[]> {
  return requestJSON('projects')
}

export function createProject(input: ProjectInput): Promise<Project> {
  return requestJSON('projects', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export function updateProject(id: string, input: ProjectInput): Promise<Project> {
  return requestJSON(`projects/${id}`, {
    method: 'PUT',
    body: JSON.stringify(input),
  })
}

export async function deleteProject(id: string): Promise<void> {
  await requestJSON<void>(`projects/${id}`, { method: 'DELETE' })
}

async function requestAsyncJob<T>(
  endpoint: string,
  init?: RequestInit,
  options?: AsyncRequestOptions,
): Promise<T> {
  const job = await requestJSON<AsyncJob<T>>(endpoint, init)
  const seenEvents = new Set<string>()
  emitNewEvents(job.events, seenEvents, options)
  const closeStream = openJobEventStream(job.id, seenEvents, options)
  try {
    return await pollJob<T>(job.id, seenEvents, options)
  } finally {
    closeStream()
  }
}

async function pollJob<T>(
  jobId: string,
  seenEvents: Set<string>,
  options?: AsyncRequestOptions,
): Promise<T> {
  const deadline = Date.now() + 180_000
  while (Date.now() < deadline) {
    await sleep(1000)
    const job = await requestJSON<AsyncJob<T>>(`jobs/${jobId}`)
    emitNewEvents(job.events, seenEvents, options)
    if (job.status === 'done') {
      if (job.result === undefined) {
        throw new Error('Background job completed without a result.')
      }
      return job.result
    }
    if (job.status === 'failed') {
      throw new Error(job.error || 'Background job failed.')
    }
  }
  throw new Error('Background job is still running after the frontend wait limit. Try again later or increase llm.timeout.')
}

function emitNewEvents(
  events: AsyncJobEvent[] | undefined,
  seenEvents: Set<string>,
  options?: AsyncRequestOptions,
): void {
  if (!events || !options?.onEvent) {
    return
  }
  for (const event of events) {
    const key = `${event.time}\n${event.message}`
    if (seenEvents.has(key)) {
      continue
    }
    seenEvents.add(key)
    options.onEvent(event)
  }
}

function openJobEventStream(
  jobId: string,
  seenEvents: Set<string>,
  options?: AsyncRequestOptions,
): () => void {
  if (!options?.onEvent || typeof EventSource === 'undefined') {
    return () => {}
  }

  const source = new EventSource(withClientId(apiPath(`jobs/${jobId}/stream`)))
  source.addEventListener('event', (message) => {
    const event = parseSSEData<AsyncJobEvent>(message)
    if (event) {
      emitNewEvents([event], seenEvents, options)
    }
  })
  source.addEventListener('done', () => source.close())
  source.addEventListener('failed', () => source.close())
  source.onerror = () => {
    source.close()
  }
  return () => source.close()
}

function parseSSEData<T>(message: MessageEvent): T | null {
  try {
    return JSON.parse(message.data) as T
  } catch {
    return null
  }
}

function withClientId(url: string): string {
  const separator = url.includes('?') ? '&' : '?'
  return `${url}${separator}clientId=${encodeURIComponent(getClientId())}`
}

function getClientId(): string {
  const key = 'ai-opencad-client-id'
  const existing = window.localStorage.getItem(key)
  if (existing) {
    return existing
  }
  const id =
    typeof crypto !== 'undefined' && 'randomUUID' in crypto
      ? crypto.randomUUID()
      : `client-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
  window.localStorage.setItem(key, id)
  return id
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms))
}

function parseResponsePayload(text: string): unknown {
  const trimmed = text.trim()
  if (!trimmed) {
    return null
  }
  try {
    return JSON.parse(trimmed)
  } catch {
    return trimmed
  }
}

function responseErrorMessage(url: string, response: Response, payload: unknown, rawText: string): string {
  if (isErrorPayload(payload)) {
    return payload.error
  }
  const text = typeof payload === 'string' ? payload : rawText.trim()
  if (text.startsWith('Proxy Error')) {
    return `Gateway returned Proxy Error for ${url}. If simple models work but complex ones fail, the gateway may be timing out. Rebuild the frontend and restart the backend so async job polling is active.`
  }
  const detail = trimForMessage(text)
  return detail ? `Request failed: HTTP ${response.status}: ${detail}` : `Request failed: HTTP ${response.status}`
}

function isErrorPayload(value: unknown): value is { error: string } {
  return (
    typeof value === 'object' &&
    value !== null &&
    'error' in value &&
    typeof (value as { error?: unknown }).error === 'string'
  )
}

function trimForMessage(value: string): string {
  const trimmed = value.trim().replace(/\s+/g, ' ')
  return trimmed.length > 220 ? `${trimmed.slice(0, 220)}...` : trimmed
}

function toMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
