import { cpSync, existsSync, mkdirSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const servedCascade = join(root, '..', 'dist', 'cascade')
const candidates = [
  join(root, 'node_modules', 'cascade-core', 'dist'),
  join(root, 'node_modules', '@zalo', 'cascade-core', 'dist'),
  join(root, '..', 'CascadeStudio', 'packages', 'cascade-core', 'dist'),
]

mkdirSync(servedCascade, { recursive: true })

const source = candidates.find((candidate) => existsSync(candidate))
if (!source) {
  console.log('cascade-core dist not found. Demo preview mode will still work.')
  process.exit(0)
}

cpSync(source, servedCascade, { recursive: true })
console.log(`Copied cascade-core assets from ${source} to ${servedCascade}`)
