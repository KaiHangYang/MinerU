# mineru-cli __VERSION__

A single-binary HTTP client for [MinerU](https://github.com/opendatalab/MinerU) document
parsing. Upload a PDF/image/Office document, wait for parsing, and download the Markdown +
images — against either your own self-hosted `mineru-api` server, or the official
[mineru.net cloud API](https://mineru.net/apiManage/docs). Same commands either way.

## Install

Grab the archive for your platform from the assets below, then:

**Linux / macOS**
```bash
curl -LO https://github.com/__REPO__/releases/download/__TAG__/mineru-cli_linux_amd64.tar.gz   # swap for darwin_amd64 / darwin_arm64 / linux_arm64
tar -xzf mineru-cli_*.tar.gz
sudo mv mineru-cli /usr/local/bin/
```

**Windows**
Download `mineru-cli_windows_amd64.zip`, unzip it, and run `mineru-cli.exe` (or put it on your `PATH`).

Optionally verify the download:
```bash
sha256sum -c checksums.txt --ignore-missing
```

## Quick start

Pick one backend and save it so you don't have to pass flags every time:

```bash
# Your own self-hosted mineru-api
mineru-cli config set --backend local --api-url http://<your-server>:8000
# If your API Server/Router requires authentication:
mineru-cli config set --api-key <your-api-key>

# Official mineru.net cloud API (get a token at https://mineru.net/apiManage/docs)
mineru-cli config set --backend cloud --token <your-token>
```

Parse a document end-to-end (upload, wait, download results):

```bash
mineru-cli parse document.pdf -o ./output
```

Or split it up for scripting:

```bash
mineru-cli submit document.pdf               # -> prints a job id
mineru-cli status <job-id>                   # -> check progress
mineru-cli result <job-id> -o ./output --wait
```

Useful flags on `parse`/`submit`: `--lang ch|en|...`, `--model pipeline|vlm`, `--ocr`,
`--no-formula`, `--no-table`, `--pages 1-10` (cloud or local V1). Run `mineru-cli --help`
or `mineru-cli <command> --help` for the full reference.

## MinerU 4 server support

The local backend discovers the current V1 Tasks protocol from `/v1/health`
capabilities. It submits JSON manifests to `/v1/tasks`, uploads missing source
bytes using each file's `upload.url` and headers, and downloads ZIP bundles from
`result_url`. SHA-256 preflight allows the server to reuse cached source files.
API Server and Router URLs may include a reverse-proxy path prefix.

Earlier V1 uploads/parse-jobs servers and legacy 3.x servers remain supported.
Protocol selection happens before submission; authentication or server errors
never cause a second submission through another protocol. For current Tasks,
`--model` accepts `flash`, `basic`, `standard`, or `advanced`; omitting it lets the
server select an available tier (standard → basic → flash → advanced). Legacy
`pipeline`, `hybrid-engine`, and `vlm-engine` map to basic, standard, and advanced.
Use `--api-key`, `MINERU_API_KEY`, or `config set --api-key` for a protected
self-hosted/Tasks deployment. API credentials are sent only to the API origin,
including authenticated downloads; external storage links use their own headers.
The existing `--token` / `MINERU_CLOUD_TOKEN` settings still select the official
mineru.net v4 cloud API independently.

V1 OCR language is configured on the server; `--lang`, `--no-formula`, and
`--no-table` only apply to cloud/legacy servers. V1 `--verbose` preserves all
available bundle JSON; original input files are not part of the V1 bundle.

## Downloads

| Platform | Archive |
|---|---|
| Linux amd64 | `mineru-cli_linux_amd64.tar.gz` |
| Linux arm64 | `mineru-cli_linux_arm64.tar.gz` |
| macOS amd64 (Intel) | `mineru-cli_darwin_amd64.tar.gz` |
| macOS arm64 (Apple Silicon) | `mineru-cli_darwin_arm64.tar.gz` |
| Windows amd64 | `mineru-cli_windows_amd64.zip` |
| Windows arm64 | `mineru-cli_windows_arm64.zip` |

`checksums.txt` has the SHA-256 of every archive above.

Archive names are version-free on purpose: once a release is marked "latest" (not a
pre-release), it stays downloadable without knowing the version, e.g.:
`https://github.com/__REPO__/releases/latest/download/mineru-cli_linux_amd64.tar.gz`

---
Built from commit `__SHA__` on branch `__BRANCH__`.
