# Stash TMDB Movie Matching Live Validation

Date checked: 2026-06-19

## Current Synology State

- Stash is reachable at `http://192.168.1.112:9999/graphql`.
- Reported version is `custom-tmdb-movie-match`.
- Reported hash is `tmdb-movie-match-20260619`.
- The deployed GraphQL schema exposes `movieMatchPlan`.
- Current compose file is `/volume1/docker/stash/docker-compose.yml`.
- Current image in that compose file is `ghcr.io/jethac/stash:tmdb-movie-match-20260619`.
- Current media mount is `/volume1/Media/Library/Ero:/data:ro`.
- Current Stash config mount is `/volume1/docker/stash/config:/root/.stash`.
- `jethac` can SSH to the NAS and read the compose file, but Docker operations require sudo.
- Compose backup before this deployment: `/volume1/docker/stash/docker-compose.yml.bak-tmdb-20260619-2240`.

## Deployment Notes

- Built locally with Docker Desktop, not on the Synology CPU.
- Local image tag: `ghcr.io/jethac/stash:tmdb-movie-match-20260619`.
- Image version check passed:
  - version: `custom-tmdb-movie-match`
  - hash: `tmdb-movie-match-20260619`
  - build time: `2026-06-19 13:35:25`
- Local disposable-container GraphQL introspection confirmed `movieMatchPlan`.
- GHCR push failed because the available GitHub token does not have the required package-write scope.
- Deployed by `docker save`, SSH byte-stream copy, `sudo docker load`, compose image update, and `sudo docker compose up -d`.
- Live GraphQL introspection confirmed `movieMatchPlan`.
- A live non-mutating `movieMatchPlan` request reached the resolver and failed closed with:
  - `missing TMDB token; set TMDB_BEARER_TOKEN on the server or pass tmdb_token`
- Temporary image tar files were removed after `docker load`.

## Required Before TMDB Live Test

1. Add `TMDB_BEARER_TOKEN` or `TMDB_API_READ_ACCESS_TOKEN` to the `environment` block, or use the `/movies/match` token override field for an ad hoc run.
2. Run a native dry-run plan from `/movies/match` or GraphQL.
3. Review a high-confidence candidate before applying any group/scene changes.

## Native Dry-Run Smoke Test

Use GraphQL against the deployed Stash:

```graphql
query MovieMatchPlan($input: MovieMatchPlanInput!) {
  movieMatchPlan(input: $input) {
    summary {
      total
      matched
      create_group
      update_group
      link_scene
      title
      no_match
      conflict
      error
    }
    items {
      action
      path
      detail
      candidate {
        score
        candidate {
          tmdb_id
          title
          release_date
          poster_url
        }
      }
      planned
    }
  }
}
```

Suggested variables:

```json
{
  "input": {
    "roots": ["/data"],
    "min_confidence": 0.9,
    "per_page": 20
  }
}
```

## End-to-End Acceptance Check

- Generate a plan from `/movies/match`.
- Confirm rows show parsed path hints, poster/title/date candidate context, confidence, planned fields, and alternates when available.
- Deselect at least one optional group field and confirm required `name` and `custom_fields` provenance remain selected.
- Apply one reviewed high-confidence group create or update.
- Confirm the resulting group has TMDB/IMDb URLs, image fields where selected, date/runtime/synopsis/director where selected, `movie_match_*` custom fields, localized title rows, and a scene link.
- Confirm `/movies` renders the resulting poster/title/year/localized title behavior.

## Current Blockers

- No TMDB token is available in this local shell, so the actual TMDB lookup dry-run has not been validated yet.
