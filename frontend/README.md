# Invosit Frontend

Barebones Vite + React + TypeScript app. Currently only renders the Kratos
login page at `/login`; everything else is a stub.

## Dev

Two ways to run this, mirroring how the API works.

**Native (HMR, fast iteration)** — typical day-to-day:

```bash
# from monorepo root: bring up Postgres + Redis + Kratos + API
docker compose up -d postgres redis kratos api

# install deps + run Vite dev server (binds 127.0.0.1:5173)
cd frontend
npm install
npm run dev
```

**Fully containerized (built once, served by `vite preview`, no HMR)** — for
end-to-end smoke tests:

```bash
# from monorepo root: brings up everything including a built+nginx-served frontend
docker compose up -d
```

Either way, open `http://127.0.0.1:4433/self-service/login/browser` — Kratos
will bounce you to `http://127.0.0.1:5173/login?flow=<id>` and the form
takes it from there.

## Scripts

- `npm run dev` — Vite dev server on `127.0.0.1:5173`
- `npm run build` — typecheck + production build to `dist/`
- `npm run typecheck` — `tsc --noEmit` only
- `npm run preview` — preview the production build

## Scope

Out of scope for now: registration, settings, error, and any post-login
pages. Those land as separate branches. The CLI-side loopback handoff for
`invosit login` is also a separate branch.
