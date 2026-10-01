# winsuck

`winsuck` streams selected files from Windows NTFS into a WSL ext4 destination.
The file payload is an uncompressed TAR stream over loopback TCP; it does not
create a temporary archive or read source files through WSL's `/mnt/c` mount.

## Install

On WSL/Linux with Homebrew:

```bash
brew tap bonest/tap
brew trust bonest/tap
brew install winsuck
```

Homebrew 7 requires third-party taps to be trusted before use. Homebrew installs
`winsuck.exe` next to `winsuck`, so `winsuck pull` can start the Windows sender
through WSL interoperability.

For manual Windows use, install `winsuck.exe` from PowerShell:

```powershell
irm https://raw.githubusercontent.com/bonest/winsuck/main/scripts/install.ps1 | iex
```

## Build

```bash
GOOS=linux go build -o winsuck ./cmd/winsuck
GOOS=windows go build -o winsuck.exe ./cmd/winsuck
```

Place both binaries where WSL can locate `winsuck.exe` next to `winsuck`, or put
the Windows binary on the Windows `PATH`.

## Usage

Display commands and options:

```bash
winsuck --help
winsuck pull --help
winsuck --version
```

Run the normal orchestrated transfer from WSL:

```bash
winsuck pull "C:\AOSService\PackagesLocalDirectory" ~/std-cache/fo-metadata \
  --port 9099 --exclude "**/bin/**" --exclude "**/*.dll"
```

Use `listen` and `send` when the receiver and sender must be started manually:

```bash
winsuck listen --dest ~/std-cache/fo-metadata --port 9099
winsuck.exe send --src "C:\AOSService\PackagesLocalDirectory" --port 9099
```

All commands accept `--port`; it defaults to `9099`. `--include` and
`--exclude` are repeatable glob flags. Includes select files, excludes always
win, and a source-root `.winsuckignore` adds excludes.

## Progress Feedback

Pass `--progress=json` (or `--progress=text`) to emit transfer progress on
stderr while stdout keeps the final `files=… bytes=…` summary. The default,
`--progress=none`, preserves the original quiet behavior. In JSON mode every
stderr line is a newline-delimited JSON event, including a single terminal
`error` event on failure.

```json
{"v":1,"phase":"scanning","files_done":1200,"bytes_done":52428800,"elapsed_ms":830}
{"v":1,"phase":"transferring","files_done":4200,"files_total":358000,"bytes_done":734003200,"bytes_total":21474836480,"current":"Models/Example.xpp"}
{"v":1,"phase":"done","files_done":358000,"files_total":358000,"bytes_done":21474836480,"bytes_total":21474836480,"skipped":12,"elapsed_ms":154000}
```

A calling program reads stderr, applies events in order, and stops on `done` or
`error`:

```bash
winsuck pull "C:\src" ~/cache --progress=json 2> >(while read -r line; do
  echo "$line" | jq -r '"\(.phase) \(.files_done)/\(.files_total)"'
done)
```

See [`docs/progress-protocol.md`](docs/progress-protocol.md) for the complete
contract: field reference, phase lifecycles, totals semantics, parsing rules,
and Bash/Python/Go examples.

## Project Configuration

Pass a JSON profile with `--config`:

```json
{
  "source": "C:\\AOSService\\PackagesLocalDirectory",
  "destination": "/home/me/std-cache/fo-metadata",
  "port": 9099,
  "workers": 12,
  "include": ["**/*.xpp", "**/*.xml"],
  "exclude": ["**/bin/**", "**/*.dll"],
  "update": true
}
```

CLI scalar flags override config values. CLI include and exclude values append
to the profile lists. `--update` records destination state in
`.winsuck-manifest.json` and avoids re-reading and transmitting source files
whose relative path, size, and mtime are unchanged.

See [`docs/test/manual-wsl.md`](docs/test/manual-wsl.md) for an end-to-end WSL
test and [`docs/test/winsuck.example.json`](docs/test/winsuck.example.json) for
a project-profile starting point.

## License

MIT. See [`LICENSE`](LICENSE).
