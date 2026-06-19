# Movie Match

`movie_match` is a workstation/CI helper for Plex-style movie matching in Stash. It scans Stash scenes under one or more path roots, parses folder/file hints, queries TMDB, scores candidates, and plans or applies group updates through GraphQL.

It is dry-run by default. It only writes when `--apply` is present.

## What It Does

- Reads scenes from Stash by `--root` path.
- Parses hints from paths:
  - `Title (YYYY)`
  - `Title [YYYY]`
  - `{tmdb-12345}`
  - `{imdb-tt1234567}`
  - `{edition-Director's Cut}`
- Prefers the parent folder name when it looks like a movie folder.
- Strips common quality/source tokens before matching.
- Looks up TMDB by:
  - TMDB ID;
  - IMDb ID through TMDB external-ID lookup;
  - title plus year search.
- Scores candidates using title, year, explicit provider IDs, and scene runtime, then declines matches below `--min-confidence`.
- Creates or updates Stash groups as movie records.
- Adds TMDB/IMDb URLs, poster/backdrop images, date, runtime, director, synopsis, and provenance custom fields where safe.
- Adds localized titles from TMDB.
- Links the matched group to the scene.

## Safety Defaults

- No writes without `--apply`.
- Existing group fields are not overwritten unless `--overwrite` is set.
- URLs and `movie_match_*` custom fields are additive.
- Existing different localized titles are reported as conflicts unless `--overwrite` is set.
- Matching failures produce `no_match` rows rather than writes.

## Required Environment

```powershell
$env:TMDB_BEARER_TOKEN = "your-tmdb-v3-read-access-token"
$env:STASH_API_KEY = "your-stash-api-key-if-needed"
```

## Dry Run

```powershell
go run ./scripts/movie_match `
  --endpoint http://192.168.1.112:9999/graphql `
  --root "/data/Porn [FR]" `
  --cache ./.cache/tmdb `
  --min-confidence 0.90 `
  --dry-run
```

## JSON Review Report

Use `--json` to create a structured review report that can be imported in Stash at `/movies/match`. The same page can also generate a native dry-run plan from Stash if the server has `TMDB_BEARER_TOKEN` or `TMDB_API_READ_ACCESS_TOKEN` set:

```powershell
go run ./scripts/movie_match `
  --endpoint http://192.168.1.112:9999/graphql `
  --root "/data/Porn [FR]" `
  --cache ./.cache/tmdb `
  --min-confidence 0.90 `
  --json > movie-match-fr.json
```

## Apply High-Confidence Matches

```powershell
go run ./scripts/movie_match `
  --endpoint http://192.168.1.112:9999/graphql `
  --root "/data/Porn [FR]" `
  --cache ./.cache/tmdb `
  --min-confidence 0.90 `
  --apply
```

## Multiple Roots

```powershell
go run ./scripts/movie_match `
  --endpoint http://192.168.1.112:9999/graphql `
  --root "/data/Porn [EN]" `
  --root "/data/Porn [FR]" `
  --root "/data/Porn [JP]"
```

## Overwrite Existing Metadata

Use this only after reviewing dry-run output:

```powershell
go run ./scripts/movie_match `
  --endpoint http://192.168.1.112:9999/graphql `
  --root "/data/Porn [FR]" `
  --min-confidence 0.95 `
  --apply `
  --overwrite
```

## Output

The output table includes:

- action;
- scene ID;
- source path;
- parsed hint;
- selected TMDB candidate;
- confidence score;
- target group;
- planned field changes;
- detail.

The summary line reports matched rows, group creates/updates, scene links, title updates, no-match rows, conflicts, and errors.
