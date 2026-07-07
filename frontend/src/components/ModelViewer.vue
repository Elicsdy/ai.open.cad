<template>
  <section class="viewer-shell">
    <div class="viewer-toolbar">
      <div>
        <span class="eyebrow">Preview</span>
        <strong>{{ engineLabel }}</strong>
        <p class="viewer-help">左键旋转，滚轮缩放，右键拖动平移</p>
      </div>
      <button type="button" @click="resetCamera">重置视角</button>
    </div>
    <div ref="host" class="viewer-host"></div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as THREE from 'three'
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js'

const props = defineProps<{
  scene: THREE.Group | null
  engineMode: 'cascade-core' | 'demo' | 'loading'
}>()

const host = ref<HTMLDivElement | null>(null)
let renderer: THREE.WebGLRenderer | null = null
let camera: THREE.PerspectiveCamera | null = null
let controls: OrbitControls | null = null
let sceneRoot: THREE.Scene | null = null
let currentModel: THREE.Group | null = null
let modelTarget = new THREE.Vector3(0, 0, 0)
let modelRadius = 120
let animationFrame = 0

const engineLabel = computed(() => {
  if (props.engineMode === 'cascade-core') {
    return 'Cascade Core'
  }
  if (props.engineMode === 'demo') {
    return 'Demo Preview'
  }
  return 'Loading'
})

onMounted(() => {
  if (!host.value) {
    return
  }

  sceneRoot = new THREE.Scene()
  sceneRoot.background = new THREE.Color('#0C1318')

  camera = new THREE.PerspectiveCamera(45, 1, 0.1, 5000)
  camera.up.set(0, 0, 1)
  resetCamera()

  renderer = new THREE.WebGLRenderer({ antialias: true })
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
  host.value.appendChild(renderer.domElement)
  renderer.domElement.addEventListener('contextmenu', preventContextMenu)

  controls = new OrbitControls(camera, renderer.domElement)
  controls.enableDamping = true
  controls.dampingFactor = 0.08
  controls.enablePan = true
  controls.enableZoom = true
  controls.enableRotate = true
  controls.screenSpacePanning = false
  controls.mouseButtons = {
    LEFT: THREE.MOUSE.ROTATE,
    MIDDLE: THREE.MOUSE.DOLLY,
    RIGHT: THREE.MOUSE.PAN,
  }
  controls.touches = {
    ONE: THREE.TOUCH.ROTATE,
    TWO: THREE.TOUCH.DOLLY_PAN,
  }

  const hemi = new THREE.HemisphereLight('#EFFBF5', '#1F3238', 2.5)
  sceneRoot.add(hemi)
  const key = new THREE.DirectionalLight('#FFFFFF', 2)
  key.position.set(80, -110, 160)
  sceneRoot.add(key)

  window.addEventListener('resize', resize)
  resize()
  renderLoop()
  mountModel(props.scene)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', resize)
  cancelAnimationFrame(animationFrame)
  renderer?.domElement.removeEventListener('contextmenu', preventContextMenu)
  controls?.dispose()
  renderer?.dispose()
})

watch(
  () => props.scene,
  (next) => {
    mountModel(next)
  },
)

function mountModel(model: THREE.Group | null): void {
  if (!sceneRoot) {
    return
  }
  if (currentModel) {
    sceneRoot.remove(currentModel)
  }
  currentModel = model
  if (currentModel) {
    centerModel(currentModel)
    sceneRoot.add(currentModel)
    fitCameraToModel(currentModel)
  }
}

function centerModel(model: THREE.Group): void {
  const box = new THREE.Box3().setFromObject(model)
  if (box.isEmpty()) {
    return
  }
  const center = box.getCenter(new THREE.Vector3())
  model.position.sub(center)
  model.updateMatrixWorld(true)
  const centeredBox = new THREE.Box3().setFromObject(model)
  modelTarget = centeredBox.getCenter(new THREE.Vector3())
  modelRadius = Math.max(centeredBox.getSize(new THREE.Vector3()).length() * 0.55, 40)
}

function resetCamera(): void {
  if (!camera) {
    return
  }
  const distance = modelRadius * 2.45
  camera.near = Math.max(distance / 1000, 0.1)
  camera.far = Math.max(distance * 12, 1000)
  camera.position.set(
    modelTarget.x + distance * 0.78,
    modelTarget.y - distance * 0.92,
    modelTarget.z + distance * 0.62,
  )
  camera.lookAt(modelTarget)
  camera.updateProjectionMatrix()
  if (controls) {
    controls.target.copy(modelTarget)
    controls.minDistance = Math.max(modelRadius * 0.08, 1)
    controls.maxDistance = Math.max(modelRadius * 20, 500)
    controls.update()
  }
}

function fitCameraToModel(model: THREE.Group): void {
  const box = new THREE.Box3().setFromObject(model)
  if (!box.isEmpty()) {
    modelTarget = box.getCenter(new THREE.Vector3())
    modelRadius = Math.max(box.getSize(new THREE.Vector3()).length() * 0.55, 40)
  }
  resetCamera()
}

function resize(): void {
  if (!host.value || !renderer || !camera) {
    return
  }
  const rect = host.value.getBoundingClientRect()
  camera.aspect = Math.max(rect.width, 1) / Math.max(rect.height, 1)
  camera.updateProjectionMatrix()
  renderer.setSize(rect.width, rect.height, false)
}

function renderLoop(): void {
  animationFrame = requestAnimationFrame(renderLoop)
  controls?.update()
  if (renderer && sceneRoot && camera) {
    renderer.render(sceneRoot, camera)
  }
}

function preventContextMenu(event: MouseEvent): void {
  event.preventDefault()
}
</script>
