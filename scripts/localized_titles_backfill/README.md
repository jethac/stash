# Localized Title Backfill

This helper imports localized titles into a running Stash instance through GraphQL. Run it from a workstation or CI runner, not from the Synology NAS.

The script is dry-run by default. It only writes when `--apply` is present, and it only replaces an existing different value when `--overwrite` is also present.

## CSV Format

Supported headers:

- `object_type`: optional, defaults to `GROUP`.
- `object_id`: Stash object ID. Aliases: `group_id`, `movie_id`, `id`.
- `object_name`: group/movie name used for exact `GROUP` lookup when no ID is present. Aliases: `group_name`, `movie_name`, `name`.
- `language_code`: required. Aliases: `lang`, `language`.
- `title`: required. Alias: `localized_title`.
- `source`: optional, defaults to the `--source` value.

Example:

```csv
group_name,lang,title,source
Movie A,ja,日本語タイトル,plex
Movie A,en,English Title,plex
Movie B,fr,Titre francais,manual
```

## JSON Format

The JSON input is an array using the same field names and aliases:

```json
[
  {
    "group_name": "Movie A",
    "language_code": "ja",
    "title": "日本語タイトル",
    "source": "plex"
  }
]
```

## Commands

Dry-run:

```powershell
go run ./scripts/localized_titles_backfill `
  --endpoint http://192.168.1.112:9999/graphql `
  --input .\localized_titles.csv
```

Apply only creates and non-conflicting unchanged rows:

```powershell
$env:STASH_API_KEY = "your-api-key"
go run ./scripts/localized_titles_backfill `
  --endpoint http://192.168.1.112:9999/graphql `
  --input .\localized_titles.csv `
  --apply
```

Apply and replace existing different values:

```powershell
go run ./scripts/localized_titles_backfill `
  --endpoint http://192.168.1.112:9999/graphql `
  --input .\localized_titles.csv `
  --apply `
  --overwrite
```

The script reports creates, updates, unchanged rows, conflicts, duplicate input rows, and row-level errors. It exits non-zero when conflicts or errors are present.
