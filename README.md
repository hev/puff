# tpuff

An unofficial Go CLI and TUI for [turbopuffer](https://turbopuffer.com).

## Install

### Prebuilt binaries

Download the latest release for your platform from
[GitHub Releases](https://github.com/hev/tpuff/releases), extract, and move
`tpuff` into your `PATH`.

### From source

```bash
go install github.com/hev/tpuff@latest
```

Or clone and build:

```bash
git clone https://github.com/hev/tpuff
cd tpuff
make install    # builds and installs to $(go env GOBIN) or $GOPATH/bin
                # also creates a 'tpuf' symlink for shorter typing
```

## Quick start

```bash
# First-time setup (prompts for API key + region)
tpuff env add prod

# Launch the interactive browser
tpuff            # equivalent to `tpuff browse`
```

> `TURBOPUFFER_API_KEY` / `TURBOPUFFER_REGION` env vars override the config
> file. A `-r <region>` flag on most commands overrides the env's region.

## Feature highlights

### Interactive TUI browser

Running `tpuff` opens a keyboard-driven terminal UI. Browse environments,
namespaces, documents, and schemas without leaving the terminal. Full-text
search is built in — press `/` in the documents view to BM25 search inline.

### Local search app (development preview)

`tpuff serve -n my-namespace` adds a browser interface generated from the namespace
schema. The Go host is implemented; the shared UI bundle remains private pending
redistribution approval. See [local search apps](docs/search-app.md) for the
`--ui-dir` development flow, supported operations, and local embedding.

### Full-text and vector search

```bash
# BM25 full-text search
tpuff search "pulmonary edema" -n notes --fts content

# Vector similarity search via an embedding model
tpuff search "renal failure" -n notes -m sentence-transformers/all-MiniLM-L6-v2
```

### Scan — extract unique field values

`scan` paginates through every document in a namespace and collects the
distinct values of a field, like a `SELECT DISTINCT` for turbopuffer. Useful
for understanding the shape of your data or building filter UIs.

```bash
tpuff scan -n my-namespace --field category
# streams progress to stderr, outputs a sorted JSON array to stdout
```

### Schema management

Copy schemas between namespaces, apply them from a file, or bulk-apply to
every namespace at once.

```bash
tpuff schema copy --from ns-a --to ns-b
tpuff schema apply --all -f schema.json
```

### Edit documents in `$EDITOR`

```bash
tpuff edit doc_abc123 -n my-namespace
```

Opens the document as JSON in your editor; saving writes the changes back.

### Prometheus exporter

Expose namespace metrics for Grafana/Prometheus. See
[monitoring.md](./monitoring.md) for scrape config and Docker deployment.

```bash
tpuff export              # exporter on :9876
tpuff export -A           # scrape all regions
```

### Multi-environment config

Manage multiple API keys and regions from `~/.tpuff/config.toml`. Switch with
`tpuff env use <name>` or interactively in the TUI.

## Run `tpuff --help` for the full command reference.

## License

MIT — see [LICENSE](./LICENSE).
