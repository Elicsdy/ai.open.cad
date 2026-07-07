import type {
  AsyncJob,
  AsyncJobEvent,
  GenerateCADRequest,
  GenerateCADResponse,
  HealthResponse,
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

const defaultAsyncJobWaitLimitMs = 30 * 60_000
let asyncJobWaitLimitMs = defaultAsyncJobWaitLimitMs

async function requestJSON<T>(endpoint: string, init?: RequestInit): Promise<T> {
  return request<T>(endpoint, init, { json: true })
}

export function setAsyncJobWaitLimitMs(ms: number): void {
  if (!Number.isFinite(ms) || ms <= 0) {
    return
  }
  asyncJobWaitLimitMs = Math.max(60_000, Math.floor(ms))
}

export function getAsyncJobWaitLimitMs(): number {
  return asyncJobWaitLimitMs
}

export function getHealth(): Promise<HealthResponse> {
  return requestJSON('health')
}

async function requestMultipart<T>(endpoint: string, formData: FormData): Promise<T> {
  return request<T>(endpoint, {
    method: 'POST',
    body: formData,
  })
}

async function request<T>(
  endpoint: string,
  init?: RequestInit,
  options: { json?: boolean } = {},
): Promise<T> {
  const url = apiPath(endpoint)
  const clientId = getClientId()
  const headers = new Headers(init?.headers)
  headers.set('X-AI-OpenCAD-Client-ID', clientId)
  if (options.json) {
    headers.set('Content-Type', 'application/json')
  }

  let response: Response
  try {
    response = await fetch(url, {
      ...init,
      headers,
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

export async function generateCADFromImage(
  file: File,
  prompt: string,
  options?: AsyncRequestOptions,
): Promise<GenerateCADResponse> {
  const uploadFile = await prepareImageForUpload(file, options)
  const form = new FormData()
  form.append('image', uploadFile)
  form.append('prompt', prompt)
  form.append('language', 'cascade-js')
  return requestAsyncJob<GenerateCADResponse>('generate-cad-from-image-async', undefined, options, form)
}

async function prepareImageForUpload(file: File, options?: AsyncRequestOptions): Promise<File> {
  try {
    const uploadFile = await compressImageForUpload(file)
    reportClientEvent(options, imageUploadMessage(file, uploadFile))
    return uploadFile
  } catch (error) {
    reportClientEvent(options, `Image compression skipped: ${toMessage(error)}`)
    return file
  }
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
  formData?: FormData,
): Promise<T> {
  const job = formData
    ? await requestMultipart<AsyncJob<T>>(endpoint, formData)
    : await requestJSON<AsyncJob<T>>(endpoint, init)
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
  const deadline = Date.now() + asyncJobWaitLimitMs
  while (Date.now() < deadline) {
    await sleep(500)
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
  const minutes = Math.max(1, Math.ceil(asyncJobWaitLimitMs / 60000))
  throw new Error(`Background job is still running after the ${minutes} minute frontend wait limit. Try again later or increase llm.timeout.`)
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

function reportClientEvent(options: AsyncRequestOptions | undefined, message: string): void {
  if (!options?.onEvent || !message) {
    return
  }
  options.onEvent({
    time: new Date().toISOString(),
    message,
  })
}

async function compressImageForUpload(file: File): Promise<File> {
  if (!file.type.startsWith('image/') || typeof document === 'undefined') {
    return file
  }

  const image = await loadImageElement(file)
  try {
    const maxDimension = 1600
    const scale = Math.min(1, maxDimension / Math.max(image.naturalWidth, image.naturalHeight))
    const width = Math.max(1, Math.round(image.naturalWidth * scale))
    const height = Math.max(1, Math.round(image.naturalHeight * scale))

    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height
    const context = canvas.getContext('2d')
    if (!context) {
      return file
    }
    context.fillStyle = '#ffffff'
    context.fillRect(0, 0, width, height)
    context.drawImage(image, 0, 0, width, height)

    const blob = await canvasToBlob(canvas, 'image/jpeg', 0.82)
    if (!blob || blob.size >= file.size) {
      return file
    }
    return new File([blob], jpegFileName(file.name), {
      type: 'image/jpeg',
      lastModified: file.lastModified,
    })
  } finally {
    image.remove()
  }
}

function loadImageElement(file: File): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file)
    const image = new Image()
    image.onload = () => {
      URL.revokeObjectURL(url)
      resolve(image)
    }
    image.onerror = () => {
      URL.revokeObjectURL(url)
      reject(new Error('Failed to load image for compression.'))
    }
    image.src = url
  })
}

function canvasToBlob(canvas: HTMLCanvasElement, type: string, quality: number): Promise<Blob | null> {
  return new Promise((resolve) => {
    canvas.toBlob(resolve, type, quality)
  })
}

function jpegFileName(name: string): string {
  const base = name.replace(/\.[^.]+$/, '')
  return `${base || 'image'}.jpg`
}

function imageUploadMessage(original: File, upload: File): string {
  if (upload === original) {
    return `Image upload size: ${formatBytes(original.size)}.`
  }
  return `Image compressed: ${formatBytes(original.size)} -> ${formatBytes(upload.size)}.`
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`
  }
  if (bytes < 1024 * 1024) {
    return `${Math.round(bytes / 102.4) / 10} KB`
  }
  return `${Math.round(bytes / 1024 / 102.4) / 10} MB`
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
  let streamClosed = false
  source.onopen = () => {
    reportClientEvent(options, 'Live job stream connected.')
  }
  source.addEventListener('event', (message) => {
    const event = parseSSEData<AsyncJobEvent>(message)
    if (event) {
      emitNewEvents([event], seenEvents, options)
    }
  })
  source.addEventListener('done', () => {
    streamClosed = true
    source.close()
  })
  source.addEventListener('failed', () => {
    streamClosed = true
    source.close()
  })
  source.onerror = () => {
    if (!streamClosed) {
      reportClientEvent(options, 'Live job stream disconnected; polling continues.')
    }
    streamClosed = true
    source.close()
  }
  return () => {
    streamClosed = true
    source.close()
  }
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
