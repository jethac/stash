# Stash TMDB Movie Matching Live Validation

Date checked: 2026-06-20

## Current Synology State

- Stash is reachable at `http://192.168.1.112:9999/graphql`.
- Reported version is `custom-tmdb-rematch`.
- Reported hash is `95bd4618-rematch`.
- The deployed GraphQL schema exposes `movieMatchPlan`, optional `roots`, and `scene_ids`.
- Current compose file is `/volume1/docker/stash/docker-compose.yml`.
- Current image in that compose file is `ghcr.io/jethac/stash:tmdb-rematch-20260620`.
- Current media mount is `/volume1/Media/Library/Ero:/data:ro`.
- Current Stash config mount is `/volume1/docker/stash/config:/root/.stash`.
- `jethac` can SSH to the NAS and read the compose file, but Docker operations require sudo.
- Compose backup before this deployment: `/volume1/docker/stash/docker-compose.yml.bak-tmdb-20260619-2240`.
- Additional compose backups before follow-up deployments:
  - `/volume1/docker/stash/docker-compose.yml.bak-tmdb-env-20260619`
  - `/volume1/docker/stash/docker-compose.yml.bak-rootfix-20260619`
  - `/volume1/docker/stash/docker-compose.yml.bak-hintfix-20260619`
  - `/volume1/docker/stash/docker-compose.yml.bak-rematch-20260620`
- TMDB bearer token is stored outside git in `/volume1/docker/stash/.env` with file mode `600`, and compose reads it through `env_file`.

## Initial Deployment Notes

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

## Rematch Deployment Notes

- Docker Desktop was stopped locally after it became unreliable during image builds.
- Built the rematch image on the Ubicloud host `hijinks-build-x64` using Docker Engine `29.1.3`.
- Remote image tag: `ghcr.io/jethac/stash:tmdb-rematch-20260620`.
- Image version check passed:
  - version: `custom-tmdb-rematch`
  - hash: `95bd4618-rematch`
  - build time: `2026-06-20 02:44:33`
- Deployed by compressed `docker save`, local transfer, legacy SCP upload to Synology, `sudo docker load`, compose image update, and `sudo docker compose up -d`.
- Live GraphQL introspection confirmed `MovieMatchPlanInput.scene_ids` is present and `roots` is optional.

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

Scene rematch variables:

```json
{
  "input": {
    "scene_ids": ["179"],
    "min_confidence": 0.9
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

- Individual rematch dry-run by `scene_ids: ["179"]` resolved TMDB ID `887755` with score `1.0`.
- The reviewed apply updated existing group `514` without overwrite:
  - retained Plex URL and added `https://www.themoviedb.org/movie/887755`;
  - set director `Hiromichi Hose`;
  - set `movie_match_*` provenance custom fields;
  - upserted `en` and `original` localized titles from TMDB;
  - left the existing Plex-sourced `ja` localized title untouched because it conflicts and overwrite was not enabled.
- Read-back confirmed group `514` has:
  - date `2021-10-05`;
  - duration `7206`;
  - front image path `/group/514/frontimage`;
  - scene count `1`, with scene `179` linked;
  - localized title rows for `en`, `ja`, and `original`;
  - TMDB provenance custom fields.
- A follow-up individual dry-run now returns `unchanged_group`, `scene_linked`, and one expected non-overwrite `title_conflict`.
- A bulk root dry-run against the same folder returns the same high-confidence candidate through the `roots` scanner.
- HTTP checks confirmed `/movies` and `/movies/match` both return `200`.
- Movie-list GraphQL data for the updated group includes poster path, date, localized titles, and scene count, which are the fields rendered by `/movies`.
- The in-app browser was unavailable in this Codex session, so visual browser inspection could not be performed from the tool surface.
