# AI OpenCAD

AI OpenCAD is a prototype for natural-language CAD generation.

- Backend: Go API server, OpenAI Responses API CAD agent, SQLite project storage.
- Frontend: Vue 3 + Vite + Three.js product UI.
- CAD runtime: browser-side Cascade Studio `cascade-core`, running Cascade Studio JavaScript directly.

## Quick Start

```powershell
Copy-Item config.example.json config.json
cd frontend
npm install
npm run build
```

Then start the backend:

```powershell
cd ../backend
go mod tidy
go run -buildvcs=false ./cmd/server
```

Open `http://localhost:15566/ai/open/cad/`. The Go backend serves the built static files under `/ai/open/cad/` and exposes the API under the same prefix.

If `llm.apiKey` is empty, the backend returns deterministic demo Cascade Studio JS so the UI, project saving, and preview flow can still be tested.

## LLM Setup

Create `config.json` in the project root:

```json
{
  "addr": ":15566",
  "dbPath": "./data/app.db",
  "frontendDist": "../dist",
  "llm": {
    "baseUrl": "https://api.openai.com",
    "apiKey": "your-api-key",
    "model": "gpt-5.5",
    "timeout": "120s",
    "reasoningEffort": "xhigh",
    "enableWebSearch": true
  }
}
```

LLM fields:

- `baseUrl`: OpenAI Responses API base URL. It can be a root URL, `/v1`, or the full `/v1/responses` endpoint.
- `apiKey`: API key sent as `Authorization: Bearer ...`.
- `model`: model name, default `gpt-5.5`.
- `timeout`: request timeout, default `120s`.
- `reasoningEffort`: reasoning strength for Responses API, default `xhigh`.
- `enableWebSearch`: when true, the backend gives the model the `web_search_preview` tool so it can look up real-world dimensions and references before generating CAD.

If a third-party OpenAI-compatible proxy does not support Responses API, reasoning effort, or web search, the backend will automatically retry without web search/reasoning and then fall back to Chat Completions.

Long-running CAD generation, repair, and refinement use async jobs. Job snapshots include `events`, and the frontend shows those events while waiting so users can see model attempts, fallback steps, streamed model deltas, parsing, and completion progress.

Multiple visitors are separated with an anonymous browser-scoped client ID sent as `X-AI-OpenCAD-Client-ID`. Projects and async jobs are scoped by that ID. This is isolation for shared usage, not an authentication or permissions system.

## CAD Mode

The current AI generation mode is `cascade-js`.

The model is prompted to output Cascade Studio JavaScript using APIs like:

- `Box(x, y, z, centered)`
- `Cylinder(radius, height, centered)`
- `Sphere(radius)`
- `Translate([x, y, z], shape)`
- `Rotate([axisX, axisY, axisZ], degrees, shape)`
- `Union([shape1, shape2])`
- `Difference(mainBody, [tool1, tool2])`

OpenSCAD support is kept internally for a future editor switch, but AI generation and default rendering now run Cascade Studio JS directly without OpenSCAD transpilation.

## Packaging

Use the release helper from the repository root. It sets Go caches inside `.cache/`, builds the frontend, runs GoReleaser, and moves generated zip files into `release/`.

```powershell
powershell -ExecutionPolicy Bypass -File build\release.ps1 -Target windows-amd64 -Snapshot
```

Supported targets:

- `windows-amd64`
- `windows-arm64`
- `linux-amd64`
- `linux-arm64`

Remove `-Snapshot` for a real tagged release in a valid git checkout.

## API

- `GET /ai/open/cad/health`
- `POST /ai/open/cad/generate-cad`
- `POST /ai/open/cad/repair-cad`
- `POST /ai/open/cad/refine-cad`
- `POST /ai/open/cad/generate-cad-async`
- `POST /ai/open/cad/repair-cad-async`
- `POST /ai/open/cad/refine-cad-async`
- `GET /ai/open/cad/jobs/:id`
- `GET /ai/open/cad/jobs/:id/stream`
- `GET /ai/open/cad/projects`
- `POST /ai/open/cad/projects`
- `GET /ai/open/cad/projects/:id`
- `PUT /ai/open/cad/projects/:id`
- `DELETE /ai/open/cad/projects/:id`
