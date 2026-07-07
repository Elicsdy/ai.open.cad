<template>
  <main class="app-shell">
    <aside class="sidebar panel">
      <div class="brand">
        <span class="brand-mark">AO</span>
        <div>
          <h1>AI OpenCAD</h1>
          <p>自然语言生成 Cascade Studio JS CAD</p>
        </div>
      </div>

      <section class="prompt-card">
        <label for="prompt">设计需求</label>
        <textarea
          id="prompt"
          v-model="prompt"
          placeholder="例如：做一个 60x40x20 的盒子，四角圆角，中间挖圆孔"
        />
        <button class="primary" type="button" :disabled="busy || !engineReady || !prompt.trim()" @click="handleGenerate">
          {{ busy ? '生成中...' : '生成 CAD' }}
        </button>
      </section>

      <section class="image-card">
        <div class="section-heading">
          <div>
            <span class="eyebrow">Vision</span>
            <h2>图片转 CAD</h2>
          </div>
          <button class="ghost" type="button" :disabled="!selectedImage" @click="clearSelectedImage">清除</button>
        </div>
        <label class="image-drop" for="image-upload">
          <input
            id="image-upload"
            type="file"
            accept="image/png,image/jpeg,image/webp"
            @change="handleImageSelected"
          />
          <span>{{ selectedImage ? selectedImage.name : '选择图纸或物品图片' }}</span>
          <small>图纸优先使用标注尺寸；物品图片会按外形和比例近似建模。</small>
        </label>
        <button
          class="primary"
          type="button"
          :disabled="busy || !engineReady || !selectedImage"
          @click="handleGenerateFromImage"
        >
          {{ busy ? '识别中...' : '根据图片生成 CAD' }}
        </button>
      </section>

      <section class="refine-card">
        <div class="section-heading">
          <div>
            <span class="eyebrow">Refine</span>
            <h2>修改当前模型</h2>
          </div>
          <button class="ghost" type="button" @click="refineOpen = !refineOpen">
            {{ refineOpen ? '收起' : '打开' }}
          </button>
        </div>
        <div v-if="refineOpen" class="refine-editor">
          <textarea
            v-model="refineInstruction"
            placeholder="例如：把中间孔加大到 18mm；底座加厚 5mm；四角改成更大的圆角；加四个螺丝孔。"
          />
          <div class="refine-actions">
            <button
              class="primary"
              type="button"
              :disabled="busy || !engineReady || !refineInstruction.trim() || !code.trim()"
              @click="handleRefine"
            >
              应用修改
            </button>
            <button class="ghost" type="button" :disabled="busy || !refineInstruction" @click="clearRefine">
              清空
            </button>
          </div>
          <p class="muted">会基于当前代码修改模型，并自动重新渲染。</p>
        </div>
      </section>

      <section>
        <h2>示例</h2>
        <button
          v-for="example in examples"
          :key="example"
          class="ghost full"
          type="button"
          @click="prompt = example"
        >
          {{ example }}
        </button>
      </section>

      <section>
        <div class="section-heading">
          <h2>项目</h2>
          <button class="ghost" type="button" @click="refreshProjects">刷新</button>
        </div>
        <div class="project-list">
          <div
            v-for="project in projects"
            :key="project.id"
            class="project-item"
            :class="{ active: project.id === currentProjectId }"
          >
            <button class="project-open" type="button" @click="loadProject(project)">
              <strong>{{ project.title }}</strong>
              <span>{{ formatDate(project.updatedAt) }}</span>
            </button>
            <button
              class="project-delete"
              type="button"
              :aria-label="`删除项目 ${project.title}`"
              @click="handleDeleteProject(project)"
            >
              删除
            </button>
          </div>
          <p v-if="projects.length === 0" class="muted">还没有保存项目。</p>
        </div>
      </section>
    </aside>

    <section class="workspace">
      <div class="code-panel panel">
        <div class="section-heading">
          <div>
            <span class="eyebrow">Cascade Studio JS</span>
            <h2>代码</h2>
          </div>
          <div class="actions">
            <button type="button" :disabled="busy || !engineReady" @click="handleRun">运行</button>
            <button type="button" :disabled="busy || !engineReady || !lastError" @click="handleRepair">自动修复</button>
            <button type="button" :disabled="busy" @click="handleSave">保存后台</button>
            <button type="button" :disabled="busy" @click="handleSaveLocal">保存本地</button>
            <button type="button" :disabled="!exportModel" @click="handleExport">{{ exportLabel }}</button>
          </div>
        </div>
        <textarea v-model="code" class="code-editor" spellcheck="false" />
      </div>

      <div class="preview-panel panel">
        <ModelViewer :scene="modelScene" :engine-mode="engineMode" />
        <div class="status-grid">
          <div>
            <span class="eyebrow">状态</span>
            <strong>{{ status }}</strong>
          </div>
          <div>
            <span class="eyebrow">项目</span>
            <strong>{{ currentProjectId ? '已保存' : '未保存' }}</strong>
          </div>
        </div>
        <div v-if="explanation || warnings.length" class="message info">
          <p v-if="explanation">{{ explanation }}</p>
          <p v-for="warning in warnings" :key="warning">{{ warning }}</p>
        </div>
        <div v-if="lastError" class="message error">
          <strong>渲染或请求失败</strong>
          <p>{{ lastError }}</p>
        </div>
        <div class="logs">
          <span v-for="log in logs" :key="log">{{ log }}</span>
        </div>
      </div>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, markRaw, onMounted, ref, shallowRef } from 'vue'
import type * as THREE from 'three'
import {
  createProject,
  deleteProject,
  generateCAD,
  generateCADFromImage,
  listProjects,
  refineCAD,
  repairCAD,
  updateProject,
} from '@/api/client'
import ModelViewer from '@/components/ModelViewer.vue'
import {
  createCascadeEngine,
  createFallbackCascadeEngine,
  type CascadeEngineAdapter,
  type ModelExport,
} from '@/composables/useCascadeEngine'
import type { AsyncJobEvent, Project } from '@/types/api'

const examples = [
  '做一个 60x40x20 的盒子，四角圆角，中间挖圆孔',
  '做一个桌面手机支架，底座稳定，前面有挡边和充电线槽',
  '做一个法兰盘，外径 84mm，中间孔 18mm，四个螺栓孔',
]

const prompt = ref('')
const code = ref(`// Cascade Studio JS mode.
// Click "Generate CAD" to start, or edit JS and run directly.

var body = Box(20, 20, 20, true);
var cut = Sphere(12);
Difference(body, [cut]);`)
const explanation = ref('')
const warnings = ref<string[]>([])
const logs = ref<string[]>([])
const lastError = ref('')
const status = ref('Demo 预览引擎已就绪，正在初始化真实 CAD 引擎')
const busy = ref(false)
const projects = ref<Project[]>([])
const currentProjectId = ref<string | null>(null)
const modelScene = shallowRef<THREE.Group | null>(null)
const engineMode = ref<'cascade-core' | 'demo' | 'loading'>('demo')
const engineReady = computed(() => engineMode.value !== 'loading')
const exportModel = ref<(() => Promise<ModelExport>) | null>(null)
const exportExtension = ref<'step' | 'stl'>('stl')
const exportLabel = computed(() => (exportExtension.value === 'step' ? '导出 STEP' : '导出 STL'))
const refineOpen = ref(false)
const refineInstruction = ref('')
const modelStreamText = ref('')
const selectedImage = ref<File | null>(null)

let engine: CascadeEngineAdapter | null = createFallbackCascadeEngine()
let evaluationRunId = 0

onMounted(async () => {
  window.localStorage.removeItem('ai-opencad-llm-settings')
  await Promise.all([initEngine(), refreshProjects()])
})

async function initEngine(): Promise<void> {
  try {
    status.value = '正在初始化真实 CAD 引擎'
    engine = await createCascadeEngine()
    engineMode.value = engine.mode
    status.value = engine.mode === 'cascade-core' ? '真实 CAD 引擎已就绪' : 'Demo 预览引擎已就绪'
  } catch (error) {
    engine = createFallbackCascadeEngine()
    engineMode.value = 'demo'
    status.value = 'Demo 预览引擎已就绪'
    lastError.value = toMessage(error)
  }
}

async function handleGenerate(): Promise<void> {
  await runBusy(async () => {
    status.value = '正在请求后台模型'
    logs.value = []
    modelStreamText.value = ''
    const response = await generateCAD(
      {
        prompt: prompt.value,
        language: 'cascade-js',
        projectId: currentProjectId.value ?? undefined,
      },
      { onEvent: handleJobEvent },
    )
    code.value = response.code
    explanation.value = response.explanation
    warnings.value = response.warnings ?? []
    await evaluateCodeWithAutoRepair('Generated')
  })
}

async function handleGenerateFromImage(): Promise<void> {
  if (!selectedImage.value) {
    return
  }
  await runBusy(async () => {
    status.value = '正在识别图片并生成 CAD'
    logs.value = []
    modelStreamText.value = ''
    const response = await generateCADFromImage(selectedImage.value as File, prompt.value, {
      onEvent: handleJobEvent,
    })
    code.value = response.code
    explanation.value = response.explanation
    warnings.value = response.warnings ?? []
    currentProjectId.value = null
    await evaluateCodeWithAutoRepair('Image-generated')
  })
}

async function handleRefine(): Promise<void> {
  await runBusy(async () => {
    status.value = '正在修改当前模型'
    logs.value = []
    modelStreamText.value = ''
    const response = await refineCAD(
      {
        prompt: prompt.value,
        code: code.value,
        instruction: refineInstruction.value,
      },
      { onEvent: handleJobEvent },
    )
    code.value = response.code
    warnings.value = response.changes
    explanation.value = '已根据当前模型修改要求更新代码。'
    await evaluateCodeWithAutoRepair('Refined')
  })
}

async function handleRun(): Promise<void> {
  await runBusy(evaluateCode)
}

async function handleRepair(): Promise<void> {
  await runBusy(async () => {
    status.value = '正在请求后台模型修复'
    logs.value = []
    modelStreamText.value = ''
    const response = await repairCAD(
      {
        prompt: prompt.value,
        code: code.value,
        error: lastError.value,
        logs: logs.value,
      },
      { onEvent: handleJobEvent },
    )
    code.value = response.code
    warnings.value = response.changes
    await evaluateCode()
  })
}

async function evaluateCodeWithAutoRepair(sourceLabel: string): Promise<void> {
  let firstError = ''
  try {
    await evaluateCode()
    return
  } catch (error) {
    firstError = toMessage(error)
    lastError.value = firstError
    logs.value = [firstError, ...logs.value]
    status.value = 'AI CAD code failed; repairing once'
  }

  const response = await repairCAD(
    {
      prompt: prompt.value,
      code: code.value,
      error: firstError,
      logs: logs.value.length > 0 ? logs.value : [firstError],
    },
    { onEvent: handleJobEvent },
  )
  code.value = response.code
  warnings.value = [
    ...warnings.value,
    `${sourceLabel} CAD JS failed on first render; auto-repaired once.`,
    ...response.changes,
  ]
  status.value = 'Re-running repaired CAD code'

  try {
    await evaluateCode()
  } catch (error) {
    throw new Error(
      `${sourceLabel} CAD JS render failed. Auto repair also failed: ${toMessage(error)}\nOriginal render error: ${firstError}`,
    )
  }
}

function handleJobEvent(event: AsyncJobEvent): void {
  if (event.message.startsWith('MODEL_DELTA ')) {
    modelStreamText.value += event.message.slice('MODEL_DELTA '.length)
    const preview = modelStreamText.value.replace(/\s+/g, ' ').slice(-500)
    const message = `${formatJobEventTime(event.time)} Model streaming: ${preview}`
    logs.value = [message, ...logs.value.filter((log) => !log.includes(' Model streaming: '))].slice(0, 80)
    status.value = 'Model is streaming response'
    return
  }
  const message = `${formatJobEventTime(event.time)} ${event.message}`
  if (!logs.value.includes(message)) {
    logs.value = [message, ...logs.value].slice(0, 80)
  }
  status.value = event.message
}

async function handleSave(): Promise<void> {
  await runBusy(async () => {
    const input = {
      title: makeTitle(prompt.value),
      prompt: prompt.value,
      code: code.value,
      language: 'cascade-js' as const,
    }
    const saved = currentProjectId.value
      ? await updateProject(currentProjectId.value, input)
      : await createProject(input)
    currentProjectId.value = saved.id
    status.value = '项目已保存'
    await refreshProjects()
  })
}

function clearRefine(): void {
  refineInstruction.value = ''
}

function handleImageSelected(event: Event): void {
  const input = event.target as HTMLInputElement
  selectedImage.value = input.files?.[0] ?? null
}

function clearSelectedImage(): void {
  selectedImage.value = null
  const input = document.getElementById('image-upload') as HTMLInputElement | null
  if (input) {
    input.value = ''
  }
}

async function handleSaveLocal(): Promise<void> {
  await runBusy(async () => {
    const title = makeTitle(prompt.value)
    const fileName = `${safeFileName(title)}.aiopencad.json`
    const payload = {
      app: 'AI OpenCAD',
      version: 1,
      title,
      prompt: prompt.value,
      code: code.value,
      language: 'cascade-js',
      explanation: explanation.value,
      warnings: warnings.value,
      savedAt: new Date().toISOString(),
    }
    const blob = new Blob([JSON.stringify(payload, null, 2)], {
      type: 'application/json;charset=utf-8',
    })

    const showSaveFilePicker = window.showSaveFilePicker
    if (showSaveFilePicker) {
      const handle = await showSaveFilePicker({
        suggestedName: fileName,
        types: [
          {
            description: 'AI OpenCAD Project',
            accept: {
              'application/json': ['.aiopencad.json', '.json'],
            },
          },
        ],
      })
      const writable = await handle.createWritable()
      await writable.write(blob)
      await writable.close()
    } else {
      downloadBlob(blob, fileName)
    }

    status.value = '项目已保存到本地'
    logs.value = [`Saved local project ${fileName}.`, ...logs.value]
  })
}

async function handleExport(): Promise<void> {
  if (!exportModel.value) {
    return
  }
  const result = await exportModel.value()
  const fileName = `${makeTitle(prompt.value)}.${result.extension}`
  downloadBlob(result.blob, fileName)
  status.value = `已导出 ${result.extension.toUpperCase()}`
  logs.value = [`Exported ${fileName} (${Math.round(result.blob.size / 1024)} KB).`, ...logs.value]
}

async function refreshProjects(): Promise<void> {
  projects.value = await listProjects().catch(() => [])
}

async function handleDeleteProject(project: Project): Promise<void> {
  const ok = window.confirm(`确认删除项目“${project.title}”？删除后无法从后台项目列表恢复。`)
  if (!ok) {
    return
  }
  await runBusy(async () => {
    await deleteProject(project.id)
    if (currentProjectId.value === project.id) {
      currentProjectId.value = null
    }
    status.value = '项目已删除'
    await refreshProjects()
  })
}

function loadProject(project: Project): void {
  currentProjectId.value = project.id
  prompt.value = project.prompt
  code.value = project.code
  explanation.value = ''
  warnings.value = []
  lastError.value = ''
  void handleRun()
}

async function evaluateCode(): Promise<void> {
  if (!engine) {
    throw new Error('CAD engine is still starting. Please try again in a moment.')
  }
  const runId = ++evaluationRunId
  status.value = '正在渲染'
  lastError.value = ''
  logs.value = []
  exportModel.value = null
  let result
  try {
    result = await engine.evaluate(code.value, 'cascade-js')
  } catch (error) {
    if (runId !== evaluationRunId) {
      return
    }
    throw error
  }
  if (runId !== evaluationRunId) {
    return
  }
  modelScene.value = markRaw(result.scene)
  logs.value = result.logs
  exportModel.value = result.exportModel ?? null
  exportExtension.value = engineMode.value === 'cascade-core' ? 'step' : 'stl'
  lastError.value = ''
  status.value = '渲染完成'
}

async function runBusy(task: () => Promise<void>): Promise<void> {
  if (busy.value) {
    return
  }
  busy.value = true
  try {
    await task()
  } catch (error) {
    lastError.value = toMessage(error)
    status.value = '需要处理错误'
  } finally {
    busy.value = false
  }
}

function makeTitle(value: string): string {
  const trimmed = value.trim()
  return trimmed.length > 24 ? `${trimmed.slice(0, 24)}...` : trimmed || 'Untitled CAD Project'
}

function safeFileName(value: string): string {
  return value
    .replace(/[<>:"/\\|?*\u0000-\u001F]/g, '_')
    .replace(/\s+/g, '_')
    .replace(/_+/g, '_')
    .slice(0, 80)
    .replace(/[. ]+$/g, '') || 'ai-opencad-project'
}

function downloadBlob(blob: Blob, fileName: string): void {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = fileName
  anchor.style.display = 'none'
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value))
}

function formatJobEventTime(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(new Date(value))
}

function toMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
</script>
