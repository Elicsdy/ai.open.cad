export type CADLanguage = 'cascade-js' | 'openscad'

export interface GenerateCADRequest {
  prompt: string
  language: CADLanguage
  projectId?: string
}

export interface GenerateCADResponse {
  code: string
  explanation: string
  warnings: string[]
}

export interface RepairCADRequest {
  prompt: string
  code: string
  error: string
  logs: string[]
}

export interface RepairCADResponse {
  code: string
  changes: string[]
}

export interface RefineCADRequest {
  prompt: string
  code: string
  instruction: string
}

export interface RefineCADResponse {
  code: string
  changes: string[]
}

export interface AsyncJob<T> {
  id: string
  kind: string
  status: 'queued' | 'running' | 'done' | 'failed'
  result?: T
  error?: string
  events: AsyncJobEvent[]
  createdAt: string
  updatedAt: string
}

export interface AsyncJobEvent {
  time: string
  message: string
}

export interface HealthResponse {
  ok: boolean
  demoMode: boolean
  agentMode: boolean
  llmModel: string
  reasoning: string
  webSearch: boolean
  webSearchTool: string
  requireWebSearch: boolean
  llmTimeoutSeconds: number
  serverTime: string
  application: string
}

export interface Project {
  id: string
  title: string
  prompt: string
  code: string
  language: CADLanguage
  createdAt: string
  updatedAt: string
}

export interface ProjectInput {
  title: string
  prompt: string
  code: string
  language: CADLanguage
}
