import * as THREE from 'three'
import { CascadeEngine, OpenSCADTranspiler } from 'cascade-core'
import type { CADLanguage } from '@/types/api'
import { assetPath } from '@/api/paths'

export interface EngineResult {
  scene: THREE.Group
  logs: string[]
  exportModel?: () => Promise<ModelExport>
}

export interface ModelExport {
  blob: Blob
  extension: 'step' | 'stl'
  mimeType: string
}

export interface CascadeEngineAdapter {
  mode: 'cascade-core' | 'demo'
  evaluate: (code: string, language?: CADLanguage) => Promise<EngineResult>
}

type CascadeEngineInstance = {
  init: () => Promise<void>
  evaluate: (code: string, options?: Record<string, unknown>) => Promise<unknown>
  exportSTEP?: () => Promise<string>
  on?: (event: string, handler: (payload: unknown) => void) => void
  _worker?: Worker
}

type CascadeFace = {
  vertex_coord: number[]
  normal_coord?: number[]
  tri_indexes: number[]
  face_index?: number
}

type CascadeEdge = {
  vertex_coord: number[]
  edge_index?: number
}

type CascadeResult = {
  meshData?: {
    faces?: CascadeFace[]
    edges?: CascadeEdge[]
  } | null
}

type OpenSCADArg = {
  name?: string
  value?: unknown
}

type OpenSCADModuleDeclaration = {
  name: string
  definitionArgs?: OpenSCADArg[]
  stmt?: unknown
}

type OpenSCADModuleInstantiation = {
  name: string
  args?: OpenSCADArg[]
}

type OpenSCADTranspilerInternals = OpenSCADTranspiler & {
  _extractParams?: (definitionArgs?: OpenSCADArg[]) => string
  _indentBlock?: (code: string) => string
  _transpileExpr?: (expr: unknown) => string
  _transpileStatement?: (stmt: unknown) => string
  _transpileModuleDeclaration?: (stmt: OpenSCADModuleDeclaration) => string
  _transpileUserModule?: (stmt: OpenSCADModuleInstantiation, children: unknown[]) => string
}

export async function createCascadeEngine(): Promise<CascadeEngineAdapter> {
  const engine = await tryCreateRealCascadeEngine()
  if (engine) {
    const transpiler = createOpenSCADTranspiler()
    const runtimeLogs: string[] = []
    engine._worker?.addEventListener('error', (event) => {
      runtimeLogs.push(`Worker error: ${event.message}`)
    })
    engine._worker?.addEventListener('messageerror', () => {
      runtimeLogs.push('Worker message error: failed to deserialize a worker response.')
    })
    engine.on?.('log', (payload) => runtimeLogs.push(formatWorkerPayload(payload)))
    engine.on?.('error', (payload) => runtimeLogs.push(formatWorkerPayload(payload)))
    engine.on?.('Progress', (payload) => {
      const progress = formatProgressPayload(payload)
      if (progress) {
        runtimeLogs.push(progress)
      }
    })
    engine.on?.('modelHistory', (payload) => {
      const count = Array.isArray(payload) ? payload.length : 0
      runtimeLogs.push(`Model history steps: ${count}`)
    })

    return {
      mode: 'cascade-core',
      evaluate: async (code: string, language: CADLanguage = 'cascade-js') => {
        runtimeLogs.length = 0
        const executableCode = prepareExecutableCode(code, language, transpiler)
        const raw = await evaluateRealCascade(engine, executableCode, runtimeLogs)
        const meshData = extractMeshData(raw)
        if (!meshData) {
          runtimeLogs.push(summarizeExecutableCode(executableCode))
        }
        const scene = cascadeMeshDataToScene(meshData, raw, runtimeLogs)
        const meshSummary = summarizeMeshData(meshData)
        return {
          scene,
          logs: runtimeLogs.length > 0 ? [...runtimeLogs, meshSummary] : ['Rendered by real cascade-core.', meshSummary],
          exportModel: engine.exportSTEP
            ? async () => {
                const stepText = await engine.exportSTEP!()
                if (!stepText?.trim()) {
                  throw new Error('Cascade Core returned an empty STEP export.')
                }
                return {
                  blob: new Blob([stepText], { type: 'application/step' }),
                  extension: 'step',
                  mimeType: 'application/step',
                }
              }
            : undefined,
        }
      },
    }
  }

  return createDemoEngine()
}

export function createFallbackCascadeEngine(): CascadeEngineAdapter {
  return createDemoEngine()
}

function createOpenSCADTranspiler(): OpenSCADTranspiler {
  const transpiler = new OpenSCADTranspiler() as OpenSCADTranspilerInternals
  if (
    typeof transpiler._extractParams !== 'function' ||
    typeof transpiler._indentBlock !== 'function' ||
    typeof transpiler._transpileExpr !== 'function' ||
    typeof transpiler._transpileStatement !== 'function'
  ) {
    return transpiler
  }

  // cascade-core's current transpiler emits children() calls but does not pass
  // OpenSCAD child modules into user-defined modules. Patch that bridge here.
  transpiler._transpileModuleDeclaration = function (
    this: OpenSCADTranspilerInternals,
    stmt: OpenSCADModuleDeclaration,
  ): string {
    const hasChildrenParam = (stmt.definitionArgs ?? []).some((arg) => arg.name === 'children')
    const params = this._extractParams?.(stmt.definitionArgs) ?? ''
    const allParams = hasChildrenParam
      ? params
      : [params, 'children = function () {}'].filter(Boolean).join(', ')
    const body = stmt.stmt ? this._transpileStatement?.(stmt.stmt) ?? '' : ''
    return `function ${stmt.name}(${allParams}) {\n${this._indentBlock?.(body) ?? body}\n}`
  }

  transpiler._transpileUserModule = function (
    this: OpenSCADTranspilerInternals,
    stmt: OpenSCADModuleInstantiation,
    children: unknown[] = [],
  ): string {
    const args = (stmt.args ?? []).map((arg) => {
      if (arg.name) {
        return this._transpileExpr?.(arg.value) ?? 'undefined'
      }
      return this._transpileExpr?.(arg.value ?? arg) ?? 'undefined'
    })

    if (children.length > 0) {
      const childBody = children
        .map((child) => this._transpileStatement?.(child) ?? '')
        .filter(Boolean)
        .join('\n')
      args.push(`function () {\n${this._indentBlock?.(childBody) ?? childBody}\n}`)
    }

    return `${stmt.name}(${args.join(', ')});`
  }

  return transpiler
}

async function tryCreateRealCascadeEngine(): Promise<CascadeEngineInstance | null> {
  try {
    const engine = new CascadeEngine({
      workerUrl: assetPath('cascade/cascade-worker.js'),
    }) as CascadeEngineInstance
    await withTimeout(engine.init(), 12000, 'Cascade engine initialization timed out.')
    return engine
  } catch {
    return null
  }
}

function withTimeout<T>(promise: Promise<T>, timeoutMs: number, message: string): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(() => reject(new Error(message)), timeoutMs)
    promise.then(
      (value) => {
        window.clearTimeout(timer)
        resolve(value)
      },
      (error: unknown) => {
        window.clearTimeout(timer)
        reject(error)
      },
    )
  })
}

async function evaluateRealCascade(
  engine: CascadeEngineInstance,
  executableCode: string,
  runtimeLogs: string[],
): Promise<unknown> {
  try {
    const raw = await engine.evaluate(executableCode, {
      guiState: { 'Grid?': true, 'GroundPlane?': true, MeshRes: 0.1 },
      maxDeviation: 0.1,
    })
    await new Promise((resolve) => window.setTimeout(resolve, 50))
    return raw
  } catch (error) {
    throw buildCascadeError(toMessage(error), null, runtimeLogs)
  }
}

function withFreshScene(code: string): string {
  return `(function () {
${cascadeStandardLibraryPrelude()}
try {
${indentForEval(code)}
  self.sceneShapes = sceneShapes;
} catch (error) {
  var message = (error && (error.stack || error.message)) || String(error);
  console.log("AI OpenCAD eval error: " + message);
  throw error;
}
}());`
}

function prepareExecutableCode(code: string, language: CADLanguage, transpiler: OpenSCADTranspiler): string {
  const cadCode = language === 'openscad' ? transpiler.transpile(code) : code
  return withFreshScene(cadCode)
}

function indentForEval(code: string): string {
  return code
    .split('\n')
    .map((line) => `  ${line}`)
    .join('\n')
}

function summarizeExecutableCode(code: string): string {
  const numbered = code
    .split('\n')
    .slice(0, 90)
    .map((line, index) => `${index + 1}: ${line}`)
    .join(' | ')
  return `Prepared Cascade eval code: ${numbered}`
}

function cascadeStandardLibraryPrelude(): string {
  const syncedFunctions = [
    'Box', 'Sphere', 'Cylinder', 'Cone', 'Polygon', 'Circle', 'BSpline', 'Text3D', 'Wedge',
    'ForEachSolid', 'GetNumSolidsInCompound', 'GetSolidFromCompound',
    'ForEachShell', 'ForEachFace', 'ForEachWire', 'MakeFace', 'GetWire', 'ForEachEdge', 'ForEachVertex',
    'Union', 'Difference', 'Intersection', 'RemoveInternalEdges',
    'Extrude', 'RotatedExtrude', 'Loft', 'Revolve', 'Pipe', 'Offset', 'OffsetWire',
    'FilletEdges', 'ChamferEdges',
    'Translate', 'Rotate', 'Mirror', 'Scale', 'Transform',
    'Sketch', 'SaveFile', 'Slider', 'Button', 'Checkbox', 'TextInput', 'Dropdown',
    'Edges', 'Faces', 'EdgeSelector', 'FaceSelector',
    'Volume', 'SurfaceArea', 'CenterOfMass', 'EdgeLength', 'Section',
  ]

  return `self.sceneShapes = [];
var sceneShapes = self.sceneShapes;
var externalShapes = self.externalShapes;
var oc = self.oc;
function syncSceneCall(fn, name) {
  if (typeof fn !== "function") {
    throw new Error("Cascade Studio function " + name + " is not available in the worker.");
  }
  return function () {
    self.sceneShapes = sceneShapes;
    var result = fn.apply(null, arguments);
    sceneShapes = self.sceneShapes;
    return result;
  };
}
function Remove(array, item) {
  var next = self.Remove(array, item);
  if (array === sceneShapes || array === self.sceneShapes) {
    sceneShapes = next;
    self.sceneShapes = next;
  }
  return next;
}
${syncedFunctions.map((name) => `var ${name} = syncSceneCall(self.${name}, "${name}");`).join('\n')}`
}

function createDemoEngine(): CascadeEngineAdapter {
  return {
    mode: 'demo',
    evaluate: async (code: string) => {
      const group = buildDemoScene(code)

      return {
        scene: group,
        logs: [
          `Rendered ${group.children.length} preview object(s) with the built-in demo engine.`,
          'Local demo mode parses a small Cascade Studio JS Box/Cylinder/Sphere subset.',
          'Install and wire Cascade Studio cascade-core to enable exact CAD booleans and STEP export.',
        ],
        exportModel: async () => {
          const stl = sceneToASCIISTL(group)
          return {
            blob: new Blob([stl], { type: 'model/stl' }),
            extension: 'stl',
            mimeType: 'model/stl',
          }
        },
      }
    },
  }
}

function buildDemoScene(code: string): THREE.Group {
  if (code.includes('aiopencad-demo-shape: flange')) {
    return buildFlangePreview(code)
  }
  if (code.includes('aiopencad-demo-shape: phone-stand')) {
    return buildPhoneStandPreview(code)
  }
  if (code.includes('aiopencad-demo-shape: rounded-box')) {
    return buildRoundedBoxPreview(code)
  }

  const generic = buildGenericPreview(code)
  if (generic.children.length > 0) {
    return generic
  }
  return buildRoundedBoxPreview(code)
}

function buildRoundedBoxPreview(code: string): THREE.Group {
  const group = new THREE.Group()
  const material = defaultMaterial()
  const dark = new THREE.MeshStandardMaterial({ color: '#17212b', roughness: 0.65 })
  const boxes = parseCascadeCalls(code, 'Box')
  const cylinders = parseCascadeCalls(code, 'Cylinder')
  const outerArgs = boxes[0]?.args ?? [60, 40, 20]
  const holeArgs = cylinders[0]?.args ?? [7, 28]
  const body = makeBox(numberAt(outerArgs, 0, 60), numberAt(outerArgs, 1, 40), numberAt(outerArgs, 2, 20), material)
  body.position.z = numberAt(outerArgs, 2, 20) / 2
  group.add(body)

  const hole = makeCylinder(numberAt(holeArgs, 0, 7), numberAt(holeArgs, 1, 26), dark)
  hole.rotation.x = Math.PI / 2
  hole.position.z = numberAt(outerArgs, 2, 20) / 2
  group.add(hole)
  return group
}

function buildPhoneStandPreview(code: string): THREE.Group {
  const group = new THREE.Group()
  const material = defaultMaterial()
  const boxes = parseCascadeCalls(code, 'Box')
  const baseArgs = boxes[0]?.args ?? [85, 68, 8]
  const backArgs = boxes[1]?.args ?? [85, 8, 72]
  const lipArgs = boxes[2]?.args ?? [85, 12, 12]

  const base = makeBox(numberAt(baseArgs, 0, 85), numberAt(baseArgs, 1, 68), numberAt(baseArgs, 2, 8), material)
  base.position.z = numberAt(baseArgs, 2, 8) / 2
  group.add(base)

  const back = makeBox(numberAt(backArgs, 0, 85), numberAt(backArgs, 1, 8), numberAt(backArgs, 2, 72), material)
  back.rotation.x = THREE.MathUtils.degToRad(18)
  back.position.set(0, 24, 42)
  group.add(back)

  const lip = makeBox(numberAt(lipArgs, 0, 85), numberAt(lipArgs, 1, 12), numberAt(lipArgs, 2, 12), material)
  lip.position.set(0, -28, 12)
  group.add(lip)
  return group
}

function buildFlangePreview(code: string): THREE.Group {
  const group = new THREE.Group()
  const material = defaultMaterial()
  const dark = new THREE.MeshStandardMaterial({ color: '#17212b', roughness: 0.65 })
  const cylinders = parseCascadeCalls(code, 'Cylinder')
  const diskArgs = cylinders[0]?.args ?? [42, 10]
  const hubArgs = cylinders[1]?.args ?? [18, 22]
  const disk = makeCylinder(numberAt(diskArgs, 0, 42), numberAt(diskArgs, 1, 10), material)
  disk.position.z = numberAt(diskArgs, 1, 10) / 2
  group.add(disk)

  const hub = makeCylinder(numberAt(hubArgs, 0, 18), numberAt(hubArgs, 1, 22), material)
  hub.position.z = numberAt(hubArgs, 1, 22) / 2
  group.add(hub)
  addBoltMarkers(group, Math.max(numberAt(diskArgs, 0, 42) * 0.62, 8), dark)
  return group
}

function buildGenericPreview(code: string): THREE.Group {
  const group = new THREE.Group()
  const material = defaultMaterial()
  for (const call of parseCascadeCalls(code, 'Box')) {
    const mesh = makeBox(numberAt(call.args, 0, 30), numberAt(call.args, 1, 30), numberAt(call.args, 2, 30), material)
    applyTransform(mesh, call.tail)
    group.add(mesh)
  }
  for (const call of parseCascadeCalls(code, 'Cylinder')) {
    const mesh = makeCylinder(numberAt(call.args, 0, 10), numberAt(call.args, 1, 30), material)
    applyTransform(mesh, call.tail)
    group.add(mesh)
  }
  for (const call of parseCascadeCalls(code, 'Sphere')) {
    const mesh = new THREE.Mesh(new THREE.SphereGeometry(numberAt(call.args, 0, 15), 48, 24), material)
    applyTransform(mesh, call.tail)
    group.add(mesh)
  }
  for (const call of parseOpenSCADCalls(code, 'cube')) {
    const mesh = makeBox(numberAt(call.args, 0, 30), numberAt(call.args, 1, 30), numberAt(call.args, 2, numberAt(call.args, 0, 30)), material)
    group.add(mesh)
  }
  for (const call of parseOpenSCADCalls(code, 'cylinder')) {
    const mesh = makeCylinder(numberAt(call.args, 0, 10), numberAt(call.args, 1, 30), material)
    group.add(mesh)
  }
  for (const call of parseOpenSCADCalls(code, 'sphere')) {
    group.add(new THREE.Mesh(new THREE.SphereGeometry(numberAt(call.args, 0, 15), 48, 24), material))
  }
  return group
}

function makeBox(x: number, y: number, z: number, material: THREE.Material): THREE.Mesh {
  return new THREE.Mesh(new THREE.BoxGeometry(x, y, z), material)
}

function makeCylinder(radius: number, height: number, material: THREE.Material): THREE.Mesh {
  const mesh = new THREE.Mesh(new THREE.CylinderGeometry(radius, radius, height, 96), material)
  mesh.rotation.x = Math.PI / 2
  return mesh
}

function addBoltMarkers(group: THREE.Group, radius: number, markerMaterial: THREE.Material): void {
  for (const angle of [0, Math.PI / 2, Math.PI, (Math.PI * 3) / 2]) {
    const marker = makeCylinder(3.2, 13, markerMaterial)
    marker.position.set(Math.cos(angle) * radius, Math.sin(angle) * radius, 6)
    group.add(marker)
  }
}

function defaultMaterial(): THREE.MeshStandardMaterial {
  return new THREE.MeshStandardMaterial({
    color: new THREE.Color('#6BB7A8'),
    metalness: 0.1,
    roughness: 0.42,
  })
}

type ParsedCall = {
  args: number[]
  tail: string
}

function parseCascadeCalls(code: string, name: string): ParsedCall[] {
  const pattern = new RegExp(`${name}\\s*\\(([^)]*)\\)([^;]*)`, 'g')
  return Array.from(code.matchAll(pattern)).map((match) => ({
    args: parseNumbers(match[1]),
    tail: match[2] ?? '',
  }))
}

function parseOpenSCADCalls(code: string, name: string): ParsedCall[] {
  const pattern = new RegExp(`${name}\\s*\\(([^;]*)\\)`, 'gi')
  return Array.from(code.matchAll(pattern)).map((match) => {
    const body = match[1]
    const namedRadius = /(?:^|[,\\s])r\\s*=\\s*(-?\\d+(?:\\.\\d+)?)/i.exec(body)
    const namedHeight = /(?:^|[,\\s])h\\s*=\\s*(-?\\d+(?:\\.\\d+)?)/i.exec(body)
    if (name === 'cylinder' && (namedRadius || namedHeight)) {
      return { args: [Number(namedRadius?.[1] ?? 10), Number(namedHeight?.[1] ?? 30)], tail: '' }
    }
    return { args: parseNumbers(body), tail: '' }
  })
}

function parseNumbers(value: string): number[] {
  return Array.from(value.matchAll(/-?\d+(?:\.\d+)?/g))
    .map((match) => Number(match[0]))
    .filter((number) => Number.isFinite(number))
}

function numberAt(values: number[], index: number, fallback: number): number {
  const value = values[index]
  return Number.isFinite(value) && value > 0 ? value : fallback
}

function applyTransform(mesh: THREE.Object3D, tail: string): void {
  const translate = /\.translate\s*\(\s*\[([^\]]*)\]\s*\)/.exec(tail)
  if (translate) {
    const [x = 0, y = 0, z = 0] = parseNumbers(translate[1])
    mesh.position.set(x, y, z)
  }
  const rotate = /\.rotate\s*\(\s*\[([^\]]*)\]\s*\)/.exec(tail)
  if (rotate) {
    const [x = 0, y = 0, z = 0] = parseNumbers(rotate[1])
    mesh.rotation.x += x
    mesh.rotation.y += y
    mesh.rotation.z += z
  }
}

function sceneToASCIISTL(group: THREE.Group): string {
  group.updateMatrixWorld(true)
  const lines = ['solid aiopencad']
  const normal = new THREE.Vector3()
  const a = new THREE.Vector3()
  const b = new THREE.Vector3()
  const c = new THREE.Vector3()

  group.traverse((object) => {
    if (!(object instanceof THREE.Mesh) || !(object.geometry instanceof THREE.BufferGeometry)) {
      return
    }
    const geometry = object.geometry.clone()
    geometry.applyMatrix4(object.matrixWorld)
    const position = geometry.getAttribute('position')
    const index = geometry.getIndex()
    const writeTriangle = (ia: number, ib: number, ic: number) => {
      a.fromBufferAttribute(position, ia)
      b.fromBufferAttribute(position, ib)
      c.fromBufferAttribute(position, ic)
      normal.subVectors(c, b).cross(new THREE.Vector3().subVectors(a, b)).normalize()
      lines.push(`  facet normal ${formatSTLNumber(normal.x)} ${formatSTLNumber(normal.y)} ${formatSTLNumber(normal.z)}`)
      lines.push('    outer loop')
      lines.push(`      vertex ${formatSTLNumber(a.x)} ${formatSTLNumber(a.y)} ${formatSTLNumber(a.z)}`)
      lines.push(`      vertex ${formatSTLNumber(b.x)} ${formatSTLNumber(b.y)} ${formatSTLNumber(b.z)}`)
      lines.push(`      vertex ${formatSTLNumber(c.x)} ${formatSTLNumber(c.y)} ${formatSTLNumber(c.z)}`)
      lines.push('    endloop')
      lines.push('  endfacet')
    }
    if (index) {
      for (let i = 0; i < index.count; i += 3) {
        writeTriangle(index.getX(i), index.getX(i + 1), index.getX(i + 2))
      }
    } else {
      for (let i = 0; i < position.count; i += 3) {
        writeTriangle(i, i + 1, i + 2)
      }
    }
    geometry.dispose()
  })
  lines.push('endsolid aiopencad')
  return lines.join('\n')
}

function formatSTLNumber(value: number): string {
  return Number.isFinite(value) ? value.toFixed(6) : '0.000000'
}

function extractMeshData(raw: unknown): CascadeResult['meshData'] {
  if (isCascadeMeshData(raw)) {
    return raw
  }
  if (isRecord(raw) && isCascadeMeshData(raw.meshData)) {
    return raw.meshData
  }
  if (Array.isArray(raw) && raw.length >= 2 && Array.isArray(raw[0]) && Array.isArray(raw[1])) {
    return { faces: raw[0] as CascadeFace[], edges: raw[1] as CascadeEdge[] }
  }
  if (Array.isArray(raw) && Array.isArray(raw[0]) && Array.isArray(raw[0][0]) && Array.isArray(raw[0][1])) {
    return { faces: raw[0][0] as CascadeFace[], edges: raw[0][1] as CascadeEdge[] }
  }
  return null
}

function isCascadeMeshData(value: unknown): value is NonNullable<CascadeResult['meshData']> {
  return isRecord(value) && (Array.isArray(value.faces) || Array.isArray(value.edges))
}

function cascadeMeshDataToScene(
  meshData: CascadeResult['meshData'],
  raw: unknown,
  runtimeLogs: string[],
): THREE.Group {
  const group = new THREE.Group()
  if (!meshData) {
    throw buildCascadeError('Cascade Core returned no mesh data.', raw, runtimeLogs)
  }
  const material = new THREE.MeshStandardMaterial({
    color: '#7DC7B7',
    metalness: 0.08,
    roughness: 0.44,
  })

  for (const face of meshData.faces ?? []) {
    if (!face.vertex_coord?.length || !face.tri_indexes?.length) {
      continue
    }
    const geometry = new THREE.BufferGeometry()
    geometry.setAttribute('position', new THREE.Float32BufferAttribute(face.vertex_coord, 3))
    if (face.normal_coord?.length === face.vertex_coord.length) {
      geometry.setAttribute('normal', new THREE.Float32BufferAttribute(face.normal_coord, 3))
    } else {
      geometry.computeVertexNormals()
    }
    geometry.setIndex(face.tri_indexes)
    group.add(new THREE.Mesh(geometry, material))
  }

  const edgeMaterial = new THREE.LineBasicMaterial({
    color: '#102026',
    transparent: true,
    opacity: 0.42,
  })
  for (const edge of meshData.edges ?? []) {
    if (!edge.vertex_coord?.length) {
      continue
    }
    const geometry = new THREE.BufferGeometry()
    geometry.setAttribute('position', new THREE.Float32BufferAttribute(edge.vertex_coord, 3))
    group.add(new THREE.Line(geometry, edgeMaterial))
  }

  if (group.children.length === 0) {
    throw buildCascadeError('Cascade Core mesh did not contain renderable faces or edges.', meshData, runtimeLogs)
  }
  return group
}

function summarizeMeshData(meshData: CascadeResult['meshData']): string {
  const faces = meshData?.faces?.length ?? 0
  const edges = meshData?.edges?.length ?? 0
  const triangles = meshData?.faces?.reduce((total, face) => total + Math.floor((face.tri_indexes?.length ?? 0) / 3), 0) ?? 0
  return `Mesh: ${faces} faces, ${edges} edges, ${triangles} triangles.`
}

function buildCascadeError(message: string, raw: unknown, runtimeLogs: string[]): Error {
  const details = [
    message,
    runtimeLogs.length > 0 ? `Worker logs: ${runtimeLogs.slice(-8).join(' | ')}` : '',
    raw ? `Return summary: ${summarizeUnknown(raw)}` : '',
  ].filter(Boolean)
  return new Error(details.join('\n'))
}

function formatWorkerPayload(payload: unknown): string {
  if (typeof payload === 'string') {
    return payload
  }
  return summarizeUnknown(payload)
}

function formatProgressPayload(payload: unknown): string {
  if (!isRecord(payload)) {
    return ''
  }
  const opType = typeof payload.opType === 'string' ? payload.opType : ''
  if (!opType) {
    return ''
  }
  return `Cascade: ${opType}`
}

function summarizeUnknown(value: unknown): string {
  if (value === null) {
    return 'null'
  }
  if (value === undefined) {
    return 'undefined'
  }
  if (Array.isArray(value)) {
    return `array(len=${value.length}) ${value.slice(0, 3).map(summarizeUnknown).join(', ')}`
  }
  if (isRecord(value)) {
    const entries = Object.entries(value)
      .slice(0, 6)
      .map(([key, item]) => `${key}:${summarizeUnknown(item)}`)
    return `{${entries.join(', ')}}`
  }
  if (typeof value === 'string') {
    return value.length > 220 ? `${value.slice(0, 220)}...` : value
  }
  return String(value)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

function toMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
