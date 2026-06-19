# Stash TMDB Movie Matching Live Validation

Date checked: 2026-06-19

## Current Synology State

- Stash is reachable at `http://192.168.1.112:9999/graphql`.
- Reported version is `custom-tmdb-movie-match-hintfix`.
- Reported hash is `3cc15cc1-hintfix`.
- The deployed GraphQL schema exposes `movieMatchPlan`.
- Current compose file is `/volume1/docker/stash/docker-compose.yml`.
- Current image in that compose file is `ghcr.io/jethac/stash:tmdb-movie-match-hintfix-20260619`.
- Current media mount is `/volume1/Media/Library/Ero:/data:ro`.
- Current Stash config mount is `/volume1/docker/stash/config:/root/.stash`.
- `jethac` can SSH to the NAS and read the compose file, but Docker operations require sudo.
- Compose backup before this deployment: `/volume1/docker/stash/docker-compose.yml.bak-tmdb-20260619-2240`.
- Additional compose backups before follow-up deployments:
  - `/volume1/docker/stash/docker-compose.yml.bak-tmdb-env-20260619`
  - `/volume1/docker/stash/docker-compose.yml.bak-rootfix-20260619`
  - `/volume1/docker/stash/docker-compose.yml.bak-hintfix-20260619`
- TMDB bearer token is stored outside git in `/volume1/docker/stash/.env` with file mode `600`, and compose reads it through `env_file`.

## Deployment Notes

- Built locally with Docker Desktop, not on the Synology CPU.
- Local image tag: `ghcr.io/jethac/stash:tmdb-movie-match-hintfix-20260619`.
- Image version check passed:
  - version: `custom-tmdb-movie-match-hintfix`
  - hash: `3cc15cc1-hintfix`
  - build time: `2026-06-19 14:18:06`
- Local disposable-container GraphQL introspection confirmed `movieMatchPlan`.
- GHCR push failed because the available GitHub token does not have the required package-write scope.
- Deployed by `docker save`, SSH byte-stream copy, `sudo docker load`, compose image update, and `sudo docker compose up -d`.
- Live GraphQL introspection confirmed `movieMatchPlan`.
- Before token configuration, a live non-mutating `movieMatchPlan` request reached the resolver and failed closed with:
  - `missing TMDB token; set TMDB_BEARER_TOKEN on the server or pass tmdb_token`
- Before token configuration, a live non-mutating `movieMatchPlan` request with an intentionally invalid token scanned `/data`, parsed a scene hint, and reached TMDB:
  - action: `error`
  - path: `/data/Porn (3DCG)/Clips/1618723246633.webm`
  - parsed title: `Clips`
  - TMDB response: `http 401`
- A live dry-run against `/data/Porn (Anime)/1. Bible Black Origins {tmdb-79641}` confirmed:
  - only scene IDs `640` and `641` were scanned, proving strict root prefix filtering for folders with spaces;
  - the parser used source name `1. Bible Black Origins {tmdb-79641}`;
  - the deployed container read the TMDB token from `/volume1/docker/stash/.env`;
  - TMDB ID `79641` resolved successfully, proving API access.
- No live apply was performed because TMDB ID `79641` resolves to `Bogyó és Babóca 2. - 13 ÚJ mese`, which does not match `Bible Black Origins`.

## Required Before Live Apply

1. Correct inaccurate folder provider IDs, or choose a different small folder with a known-good `{tmdb-*}` or `{imdb-tt*}` token.
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

- The TMDB token is configured and live dry-run works.
- The tested folder had an incorrect TMDB ID, so a reviewed live apply was intentionally skipped.
