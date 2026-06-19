# Synology Deployment Notes

The Synology NAS is a runtime target only. Do not compile Go, build the UI, or build Docker images on the NAS.

## Image

GitHub Actions publishes the custom image to:

```text
ghcr.io/jethac/stash:media-library-improvements
ghcr.io/jethac/stash:sha-<short-sha>
ghcr.io/jethac/stash:custom
```

The custom deployment image is built for `linux/amd64`, matching the DS1522+ CPU. Do not use this image for ARM Synology models unless the workflow is expanded again.

Use the immutable `sha-<short-sha>` tag when testing a specific PR build. Use `media-library-improvements` when tracking the branch.

## Pre-Deploy Backup

Before changing the running container, back up the Stash config directory and database from the Synology volume. Adjust paths if the container uses a different bind mount.

```sh
stamp="$(date +%Y%m%d-%H%M%S)"
mkdir -p "/volume1/docker/backups/stash-${stamp}"
cp -a "/volume1/docker/stash/config" "/volume1/docker/backups/stash-${stamp}/config"
```

If Stash is running, stop the container before copying the database for a consistent backup:

```sh
docker stop stash
cp -a "/volume1/docker/stash/config/stash-go.sqlite" "/volume1/docker/backups/stash-${stamp}/stash-go.sqlite"
```

## Pull And Restart

Pull the completed image on the NAS. This is the only image work the NAS should do.

```sh
docker pull ghcr.io/jethac/stash:media-library-improvements
```

If the existing container is managed by Container Manager, update the image field there and recreate the container with the same volumes, ports, and environment.

If it is managed manually, recreate it with the same options currently in use. Example shape:

```sh
docker rm -f stash
docker run -d \
  --name stash \
  --restart unless-stopped \
  -p 9999:9999 \
  -v /volume1/docker/stash/config:/root/.stash \
  -v /volume1/media:/data \
  ghcr.io/jethac/stash:media-library-improvements
```

## Smoke Tests

After restart:

- Open the Stash web UI.
- Confirm the movie grid loads.
- Open a movie detail page.
- Switch the movie title language selector.
- Edit English, Japanese, or French localized title fields and save.
- Search/filter movies by localized title.
- Confirm scene playback still works.

## Rollback

If the custom image fails, stop the container and recreate it with the prior image tag. Restore the backed-up config/database only if the application migrated or corrupted runtime data.
