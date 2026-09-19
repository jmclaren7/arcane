-- +goose Up
-- Add the commit-injection opt-in to gitops_syncs.
-- See backend/internal/models/gitops_sync.go for field semantics.
-- inject_commit_env: when enabled, the sync writes ARCANE_GIT_COMMIT,
--   ARCANE_GIT_COMMIT_SHORT and ARCANE_GIT_BRANCH into the synced project's
--   Git-sourced env so the deployed application can report its commit.
ALTER TABLE gitops_syncs ADD COLUMN inject_commit_env BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE gitops_syncs DROP COLUMN inject_commit_env;
