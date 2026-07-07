/// <reference types="vite/client" />

declare module '*.vue' {
  import type { DefineComponent } from 'vue'

  const component: DefineComponent<object, object, unknown>
  export default component
}

declare module 'cascade-core' {
  export class CascadeEngine {
    constructor(options: { workerUrl: string })
    init(): Promise<void>
    evaluate(code: string, options?: Record<string, unknown>): Promise<unknown>
    exportSTEP?(): Promise<string>
    on?(event: string, handler: (payload: unknown) => void): void
  }

  export class OpenSCADTranspiler {
    transpile(code: string): string
  }
}

interface FileSystemWritableFileStream extends WritableStream {
  write(data: Blob | BufferSource | string): Promise<void>
  close(): Promise<void>
}

interface FileSystemFileHandle {
  createWritable(): Promise<FileSystemWritableFileStream>
}

interface SaveFilePickerType {
  description?: string
  accept: Record<string, string[]>
}

interface SaveFilePickerOptions {
  suggestedName?: string
  types?: SaveFilePickerType[]
}

interface Window {
  showSaveFilePicker?: (options?: SaveFilePickerOptions) => Promise<FileSystemFileHandle>
}
