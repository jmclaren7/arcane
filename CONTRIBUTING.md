  # Contributing to Arcane

Thanks for helping make Arcane better! We've built a modern, streamlined development experience that gets you up and running in minutes.

> **Using AI tools?** Please read our [AI Usage Policy](AI_POLICY.md) before contributing.

## 🌟 Ways to Contribute

- 🐛 **Report bugs** using our issue templates
- 💡 **Suggest features** or improvements
- 🔧 **Code contributions** (frontend, backend, DevOps)
- 📚 **Documentation** improvements
- 🌍 **Translations** via [Crowdin](https://crowdin.com/project/arcane-docker-management)
- 🧪 **Testing** and quality assurance

## 🚀 Quick Start

### Prerequisites

**Required**

- **Docker & Docker Compose** — runs the full dev stack via `./scripts/development/dev.sh start`
- **VS Code** based IDE (recommended for the best developer experience)

**Optional — host tools for the Justfile shortcuts**

The `Justfile` recipes (`just lint`, `just test`, `just format`, etc.) run on your host, not inside the dev containers. You only need these if you want to use them; otherwise stick to `./scripts/development/dev.sh` and the [Manual Commands](#manual-commands) examples below.

- [`just`](https://github.com/casey/just) — recipe runner
- [`pnpm`](https://pnpm.io/) (or Node 25+ with `corepack enable`) — frontend tooling
- [Go](https://go.dev/dl/) 1.26+ — backend, CLI, and types modules
- [`golangci-lint`](https://golangci-lint.run/) — Go linter used by `just lint backend|cli|types`

On Windows (via [winget](https://learn.microsoft.com/en-us/windows/package-manager/winget/)):

```powershell
winget install Casey.Just.Just pnpm.pnpm GoLang.Go golangci-lint.golangci-lint Git.Git
```

(`Git.Git` puts `bash` on PATH — the Justfile recipes use `#!/usr/bin/env bash`.)

> **💡 Working Directory**: Unless otherwise specified, all commands in this guide should be run from the project root directory (`arcane/`).

### 1. Fork and Clone

```bash
git clone https://github.com/<your-username>/arcane.git
cd arcane
```

### 2. Start Development Environment

From the project root directory:

```bash
./scripts/development/dev.sh start
```

That's it! The development environment will automatically:

- 🔥 Start both frontend and backend with hot reload
- 🐳 Handle all dependencies via Docker
- 📊 Set up health checks and monitoring
- 💾 Create persistent storage for your development data

Access your development environment:

- **Frontend**: http://localhost:3000 (SvelteKit with HMR)
- **Backend**: http://localhost:3552 (Go with Air hot reload)

## 🎯 VS Code Integration

For the best development experience, we've included VS Code tasks and workspace configuration.

### Recommended Extensions

When you open the project in VS Code, you'll be prompted to install our recommended extensions. These provide:

- Docker integration and management
- Go language support with debugging
- Svelte/TypeScript support
- Integrated terminal management

### One-Click Development Commands

Use `Ctrl/Cmd+Shift+P` → "Tasks: Run Task" to access:

| Task              | Description                                   |
| ----------------- | --------------------------------------------- |
| **Start**         | Start the development environment             |
| **Stop**          | Stop all services                             |
| **Restart**       | Restart all services                          |
| **Rebuild**       | Rebuild containers (after dependency changes) |
| **Clean**         | Remove all containers and volumes             |
| **Logs**          | Interactive log viewer with service selection |
| **Open Frontend** | Launch frontend in browser                    |

### Quick Build Shortcut

Press `Ctrl/Cmd+Shift+B` to run the default build task (Start Environment).

## 🔍 Development Workflow

### Making Changes

1. **Create a feature branch**:

   ```bash
   git switch -c feat/my-awesome-feature
   # or
   git switch -c fix/issue-123
   ```

2. **Start development** (from project root):

   ```bash
   ./scripts/development/dev.sh start
   # or use VS Code Task: "Start"
   ```

3. **Monitor logs** (choose your preferred method):

   ```bash
   # Interactive selector
   ./scripts/development/dev.sh logs

   # Specific service
   ./scripts/development/dev.sh logs frontend
   ./scripts/development/dev.sh logs backend

   # Or use VS Code Task: "Logs"
   ```

4. **Make your changes** - hot reload will automatically update:
   - **Frontend**: Instant HMR via Vite
   - **Backend**: Auto-rebuild and restart via Air

## 🛠️ Development Commands

**Note**: All commands should be run from the project root directory (`arcane/`).

### Justfile Shortcuts

We provide a categorized `Justfile` for common workflows. Run `just --list` to see every category and target. These recipes execute on your host (not inside the dev containers), so they need the optional tools listed in [Prerequisites](#prerequisites). `just dev docker` / `just dev logs` work with Docker alone.

```bash
# Development
just dev docker
just dev logs

# Build
just build single frontend
just build single backend

# Tests
just test all
just test backend

# Quality checks
just lint frontend
just format frontend
just format all --check

# Dependencies and Go modules
just deps install all
just gomod tidy all
```

### Environment Management

```bash
# Start development environment
./scripts/development/dev.sh start

# View service status
./scripts/development/dev.sh status

# Stop all services
./scripts/development/dev.sh stop

# Restart services (for config changes)
./scripts/development/dev.sh restart

# Rebuild containers (for dependency changes)
./scripts/development/dev.sh rebuild

# Clean up everything (nuclear option)
./scripts/development/dev.sh clean
```

### Debugging & Logs

```bash
# Interactive log selection
./scripts/development/dev.sh logs

# All services
./scripts/development/dev.sh logs

# Frontend only (Vite/SvelteKit)
./scripts/development/dev.sh logs frontend

# Backend only (Go/Air)
./scripts/development/dev.sh logs backend

# Shell access
./scripts/development/dev.sh shell frontend
./scripts/development/dev.sh shell backend
```

## 🎨 Code Quality

### Automatic Formatting & Linting

Both services include development-time linting and formatting:

- **Frontend**: ESLint + Prettier (configured in VS Code)
- **Backend**: Go fmt + Go vet (built into Air hot reload)

### Manual Commands

If you need to run checks manually, you have three options. Pick whichever fits.

**1. Justfile (runs on the host — needs the optional tools from [Prerequisites](#prerequisites))**

```bash
just lint frontend        # svelte-check / TypeScript check
just lint backend         # golangci-lint on the backend module
just lint all             # frontend + backend + cli + types
just format all           # write formatting fixes
just format all --check   # verify formatting without writing
```

**2. Shell into the dev container** (recommended if you only have Docker installed)

```bash
./scripts/development/dev.sh shell frontend
# inside the container:
pnpm -C frontend check
pnpm -C frontend format

./scripts/development/dev.sh shell backend
# inside the container:
go fmt ./...
go vet ./...
```

**3. One-shot `docker compose exec`**

The dev stack runs under Compose project `arcane-dev` (set by `dev.sh`), so you must pass `-p arcane-dev` or Compose won't find the running services. The frontend's `check`/`format` scripts live in `frontend/package.json`, so run them with `pnpm -C frontend`:

```bash
docker compose -f docker/compose.dev.yaml -p arcane-dev exec frontend pnpm -C frontend check
docker compose -f docker/compose.dev.yaml -p arcane-dev exec frontend pnpm -C frontend format

docker compose -f docker/compose.dev.yaml -p arcane-dev exec backend go fmt ./...
docker compose -f docker/compose.dev.yaml -p arcane-dev exec backend go vet ./...
```

## 📝 Commit Guidelines

We use **Conventional Commits** for clear, semantic commit messages:

```bash
git commit -m "feat: add user authentication"
git commit -m "fix: resolve Docker volume mounting issue"
git commit -m "docs: update development setup guide"
git commit -m "refactor: simplify API response handling"
```

**Types**: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`

## 🔄 Pull Request Process

1. **Keep changes focused** - One feature/fix per PR
2. **Test your changes** - Ensure both frontend and backend work
3. **Update documentation** - If you change APIs or add features
4. **Link issues** - Reference issues with "Closes #123" or "Fixes #456"
5. **Be responsive** - Address review feedback promptly

### PR Checklist

- [ ] Code builds successfully in development environment
- [ ] Frontend hot reload works correctly
- [ ] Backend hot reload works correctly
- [ ] No linting errors
- [ ] Commit messages follow conventional format
- [ ] PR description explains the change and why it's needed

## 🐛 Troubleshooting

### Common Issues

**Port conflicts:**

```bash
# Stop and clean everything (from project root)
./scripts/development/dev.sh clean

# Check for conflicting processes
lsof -i :3000  # Frontend port
lsof -i :3552  # Backend port
```

**Docker issues:**

```bash
# Reset Docker environment (from project root)
./scripts/development/dev.sh clean
docker system prune -f

# Restart development
./scripts/development/dev.sh start
```

**VS Code tasks not working:**

- Ensure you've opened the project root folder (`arcane/`) in VS Code, not a subfolder or parent directory
- Install recommended extensions when prompted
- Restart VS Code if tasks don't appear
- Verify you're in the correct working directory when running terminal commands

### Need Help?

- **Bug Report**: [Create an issue](https://github.com/ofkm/arcane/issues/new?template=bug.yml)
- **Feature Request**: [Suggest a feature](https://github.com/ofkm/arcane/issues/new?template=feature.yml)
- **Development Question**: Open a discussion in the repository

Thank you for contributing to Arcane! Your help makes this project better for everyone. 🚀
