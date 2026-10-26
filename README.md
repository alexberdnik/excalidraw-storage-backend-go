# excalidraw-storage-backend-go

A pure Go reimplementation of
[excalidraw-storage-backend](https://github.com/alswl/excalidraw-storage-backend)
(NestJS + Keyv).

It provides the HTTP storage API used by self-hosted Excalidraw for exported
scenes, live collaboration rooms and embedded images, and persists everything
either in a single SQLite database or as plain files (`STORE_BACKEND`).

## API

All routes are prefixed with `GLOBAL_PREFIX` (default `/api/v2`).

| Method | Path           | Description                                         | Success                                     | Errors                                      |
|--------|----------------|-----------------------------------------------------|---------------------------------------------|---------------------------------------------|
| `POST` | `/scenes`      | Store a scene, id generated server-side (16 digits) | `201` `{"id":"1234567890123456"}`           | `500` on id collision, `413` body too large |
| `GET`  | `/scenes/{id}` | Fetch a scene                                       | `200` raw bytes, `application/octet-stream` | `404`                                       |
| `PUT`  | `/rooms/{id}`  | Store a room under a client-chosen id               | `200` `{"id":"{id}"}`                       | `413`                                       |
| `GET`  | `/rooms/{id}`  | Fetch a room                                        | `200` raw bytes, `application/octet-stream` | `404`                                       |
| `PUT`  | `/files/{id}`  | Store an image/file under a client-chosen id        | `200` `{"id":"{id}"}`                       | `413`                                       |
| `GET`  | `/files/{id}`  | Fetch an image/file                                 | `200` raw bytes, `application/octet-stream` | `404`                                       |

Notes on the behavior (kept from the original implementation):

- Request bodies are read **raw**, regardless of `Content-Type`. Excalidraw
  does not send a content-type header when saving scenes or rooms.
- Uploads for rooms/files overwrite an existing value; `POST /scenes` never
  overwrites because the id is freshly generated.
- `POST /scenes` also accepts a trailing slash (`/scenes/`) — the frontend is
  usually configured with one.
- Scene ids are 16 decimal digits because the Excalidraw frontend only accepts
  numeric ids.
- CORS is fully open (`Access-Control-Allow-Origin: *`) and `OPTIONS` preflight
  requests are answered with `204`.
- Unknown routes return `404` (`{"message":"Cannot GET /api/v2/nope","statusCode":404}`);

### Examples

```sh
# create a scene
curl -X POST --data-binary @scene.json http://localhost:8080/api/v2/scenes
# => {"id":"4820491730582916"}

# read it back
curl -i http://localhost:8080/api/v2/scenes/4820491730582916

# store / read a collaborative room
curl -X PUT --data-binary @room.json http://localhost:8080/api/v2/rooms/my-room
curl http://localhost:8080/api/v2/rooms/my-room

# store / read a binary file
curl -X PUT --data-binary @image.png http://localhost:8080/api/v2/files/my-image
curl -o image.png http://localhost:8080/api/v2/files/my-image
```

## Configuration

| Variable        | Description                                                       | Default                |
|-----------------|-------------------------------------------------------------------|------------------------|
| `PORT`          | HTTP listening port                                               | `8080`                 |
| `GLOBAL_PREFIX` | Prefix for every route; set to empty to disable                   | `/api/v2`              |
| `STORE_BACKEND` | `sqlite` (one database file) or `files` (one file per blob)       | `sqlite`               |
| `SQLITE_PATH`   | SQLite database file, used when `STORE_BACKEND=sqlite`            | `./data/excalidraw.db` |
| `FILES_PATH`    | Blob directory, used when `STORE_BACKEND=files`                   | `./data/blobs`         |
| `BODY_LIMIT`    | Maximum request body size (`50mb`, `1024kb`, plain bytes, …)      | `50mb`                 |
| `LOG_LEVEL`     | `debug`, `info`/`log`, `warn`, `error` (`verbose` = `debug`)      | `info`                 |

The original `STORAGE_URI` (Keyv/Redis/Mongo/Postgres/MySQL) is intentionally
not supported: this implementation only has the two backends above and no
in-memory mode.

## Storage

`STORE_BACKEND` selects one of two interchangeable backends. The HTTP API is
identical for both; switching backends does not migrate data.

### `STORE_BACKEND=sqlite` (default)

One table:

```sql
CREATE TABLE blobs
(
    namespace TEXT NOT NULL, -- SCENES | ROOMS | FILES
    key       TEXT NOT NULL,
    value     BLOB NOT NULL,
    PRIMARY KEY (namespace, key)
) WITHOUT ROWID;
```

The database runs in WAL mode and is written through a single connection, which
serializes writes and avoids `SQLITE_BUSY` errors.

### `STORE_BACKEND=files`

Plain files, one per blob, grouped by namespace and sharded by the last two
characters of the key:

```
<FILES_PATH>/SCENES/45/12345.blob          <- scene id 12345
<FILES_PATH>/ROOMS/23/abc123.blob          <- room id abc123
<FILES_PATH>/FILES/ge/myimage.blob         <- file id myimage
```

## Build and run

Requires Go 1.24+.

```sh
make run                 # or: go run .
make test                # or: go test ./...
make build               # -> bin/server

SQLITE_PATH=/var/lib/excalidraw/db.sqlite PORT=8080 ./bin/server
STORE_BACKEND=files FILES_PATH=/var/lib/excalidraw/blobs PORT=8080 ./bin/server
```

### Docker

A prebuilt image is available on Docker Hub:
[`alexberdnik/excalidraw-storage-backend-go`](https://hub.docker.com/r/alexberdnik/excalidraw-storage-backend-go).

```sh
docker run -p 8080:8080 -v excalidraw-data:/data alexberdnik/excalidraw-storage-backend-go
```

Or build it yourself:

```sh
docker build -t excalidraw-storage-backend-go .
docker run -p 8080:8080 -v excalidraw-data:/data excalidraw-storage-backend-go

# or
docker compose up --build
```

### Excalidraw frontend

Point the frontend at this backend (the trailing slash on `BACKEND_V2_POST_URL`
is supported):

```
BACKEND_V2_GET_URL=http://localhost:8080/api/v2/scenes/
BACKEND_V2_POST_URL=http://localhost:8080/api/v2/scenes/
STORAGE_BACKEND=http
HTTP_STORAGE_BACKEND_URL=http://localhost:8080/api/v2
```

## License

MIT — see [LICENSE](LICENSE)
