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
`--no-formula`, `--no-table`, `--pages 1-10` (cloud backend only). Run `mineru-cli --help`
or `mineru-cli <command> --help` for the full reference.

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
