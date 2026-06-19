# Stash Media Library Improvements Development Plan

## Ground Rules

- Do not build on the Synology NAS. Its role is to pull and run finished Docker images.
- Use `jethac/stash` as the fork and `jethac/media-library-improvements` as the feature branch.
- Keep changes sliced into reviewable commits.
- Prefer existing Stash patterns for GraphQL, SQLite stores, React components, and generated UI types.
- Run CPU-heavy checks locally or in GitHub Actions.

## Current Branch

- Fork: `https://github.com/jethac/stash`
- Branch: `jethac/media-library-improvements`
- Draft PR: `https://github.com/jethac/stash/pull/1`

## Phase 1: Movie And Performer Foundations

Status: mostly complete.

Tasks:

- Add performer `group_count` sorting and filtering support.
- Add movie/group `performer_count` filtering and sorting support.
- Add `/movies` route as a movie-focused entry point over groups.
- Rename or expose navigation so movies are discoverable.
- Render movie/group cards in poster aspect ratio.
- Show movie metadata on cards.
- Add missing-poster card state.

Validation:

- `go generate ./cmd/stash`
- `go test ./pkg/models`
- SQLite integration tests in Docker where Windows CGO/sqlite blocks local execution.
- `pnpm run check`
- `pnpm run lint:js`
- `pnpm run build`

## Phase 2: Localized Title Data Model And API

Status: mostly complete.

Tasks:

- Add `localized_titles` SQLite table and migration.
- Add model and repository interfaces.
- Add SQLite store with create, update, upsert, destroy, and lookup methods.
- Add GraphQL types, inputs, queries, and mutations.
- Add `localized_titles` fields to group/movie and scene GraphQL types.
- Generate backend and frontend GraphQL artifacts.
- Add focused store tests and API compile/test coverage.

Validation:

- `go test ./pkg/models/...`
- Docker API test: `go test ./internal/api -count=1`
- Docker SQLite focused integration test for localized titles.
- `pnpm run gqlgen`
- `pnpm run check`

## Phase 3: Localized Title UI

Status: in progress.

Completed:

- Add localized title data to movie/group list queries.
- Add movie list title-language selector.
- Add localized title fallback helper.
- Add movie/group edit fields for English, Japanese, and French localized titles.
- Add movie/group detail title-language selector.
- Add localized title search/filter support.
- Add a missing-localized-title filter for cleanup.

Remaining tasks:

- Decide whether to add a global title-language preference in settings.
- Add localized title display to other movie detail surfaces, such as sticky/compressed headers if needed.
- Consider scene localized-title editing after movie/group workflow stabilizes.

Validation:

- `pnpm run gqlgen` after GraphQL document changes.
- `pnpm run check`
- `pnpm run lint:js`
- `pnpm exec stylelint src/components/Groups/styles.scss`
- `pnpm run build`

## Phase 4: Poster And Movie Cleanup Workflows

Status: not started.

Tasks:

- Audit existing group front/back image upload and URL flows.
- Decide whether dedicated poster controls are needed or whether existing image controls are enough.
- Add filters for missing poster and ungrouped scenes if backend support is not already sufficient.
- Add movie cleanup views for missing poster, missing title language, missing performers, and missing studio.
- Ensure bulk actions are explicit and reversible where possible.

Validation:

- Backend filter tests for any new filter operations.
- UI tests or focused manual checks for cleanup filters.
- Production UI build.

## Phase 5: Metadata Import Helpers

Status: in progress.

Completed:

- Define CSV/JSON import format for localized title backfills.
- Add `scripts/localized_titles_backfill`, a workstation-run GraphQL helper for localized title imports.
- Add dry-run mode, duplicate detection, and conflict reporting.
- Require `--apply` for writes and `--overwrite` before replacing existing different values.

Remaining tasks:

- Optionally support Plex database derived title exports where source data is reliable.
- Run a dry-run against production Stash with a real localized-title export.
- Back up production Stash config/database before any apply run.

Validation:

- `go test ./scripts/localized_titles_backfill`
- Dry-run against a copied database or fixture.
- Upsert test with duplicate language/object pairs.
- Backup and restore drill before running on production data.

## Phase 6: Docker Image And Synology Deployment

Status: in progress.

Tasks:

- Add or reuse GitHub Actions workflow for Synology amd64 Docker image builds. (Complete)
- Publish to GitHub Container Registry under `ghcr.io/jethac/stash`. (Complete)
- Tag images by branch SHA and optional semantic label. (Complete)
- On Synology, back up the Stash config directory and database.
- Pull the finished image on Synology.
- Update the Stash container image reference.
- Restart the container.
- Smoke test login, movie list, movie detail, localized title edit, and scene playback.

Synology rule:

- Never compile Go, run frontend builds, or run Docker image builds on the NAS.
- Use Synology only for `docker pull`, container restart, and runtime smoke tests.

## Recommended Next Work

1. Wait for the GHCR workflow to publish `ghcr.io/jethac/stash:media-library-improvements`.
2. Deploy the custom image to Synology after CI artifacts exist.
3. Dry-run `scripts/localized_titles_backfill` against a real export.
4. Decide whether title language should become a global user setting.
