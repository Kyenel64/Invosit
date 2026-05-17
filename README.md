<div align="center">
  <h1>Invosit</h1>
  <h3>File sync for gitignored files.</h3>
  <p>
    <a href="./CLAUDE.md">Docs</a>
    &nbsp;•&nbsp;
    <a href="./docs/openapi.yaml">API spec</a>
    &nbsp;•&nbsp;
    <a href="https://github.com/kyenel64/invosit/issues">Issues</a>
  </p>
  <p>
    <a href="https://github.com/kyenel64/invosit/actions/workflows/ci.yml">
      <img src="https://github.com/kyenel64/invosit/actions/workflows/ci.yml/badge.svg" alt="CI">
    </a>
    <a href="https://github.com/kyenel64/invosit/actions/workflows/security.yml">
      <img src="https://github.com/kyenel64/invosit/actions/workflows/security.yml/badge.svg" alt="Security">
    </a>
    <a href="./LICENSE">
      <img src="https://img.shields.io/badge/license-Apache%202.0-blue.svg" alt="License: Apache 2.0">
    </a>
    <img src="https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.26+">
  </p>
</div>

## Introduction
Invosit lets devs/teams push and pull gitignored files securely alongside a repository, with access control.

If your team is still sharing .env, CLAUDE.md, or docs through Slack or shared storage, Invosit makes things much simpler. Just run `invosit pull` after `git clone` or `git pull` to fetch the gitignored files you have access to.

**Security is core.** Invosit follows modern security best practices to
keep your data as safe as possible. We want you to feel confident putting
your most sensitive files in Invosit.

## Features

- **End-to-end encryption.** Files are encrypted on your machine before they ever leave it. AES-256-GCM with a fresh per-file key. Invosit's servers only ever see ciphertext.
- **Workspaces and roles.** Group files by team or project. Admin, member, and viewer roles map to the access levels you'd expect.
- **Cryptographic access control.** Each authorized user holds a wrapped copy of the file key. Revoking a user deletes their wrap and their next pull returns 403 instantly, with no re-encryption needed.
- **Environments.** Separate dev, staging, and prod environments within a workspace.
- **Self-hostable.** Host Invosit yourself if Invosit Cloud isn't your match.
- **Versioning and rollback.** Every push creates a new version. Roll back to any prior version with a single command.
- **Audit log.** Every push, pull, grant, revocation, and rollback is recorded and queryable per workspace.


## Getting Started

```bash
git clone https://github.com/kyenel64/invosit
cd invosit
cp .env.example .env # fill in storage credentials and Kratos secrets
docker compose up -d # start Postgres, Redis, Kratos
```

## License

Licensed under the [Apache License, Version 2.0](./LICENSE).
