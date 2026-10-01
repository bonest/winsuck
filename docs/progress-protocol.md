# Progress Protocol

This document is the integration contract for programs that run `winsuck` and
want to render a status bar. Progress is opt-in and machine-readable.

## Invocation

```
winsuck pull  <source> <destination> --progress=json [flags]
winsuck send  <source>              --progress=json [flags]
winsuck listen <destination>        --progress=json [flags]
```

Modes:

| Mode | Behavior |
| --- | --- |
| `none` (default) | No progress output; original quiet behavior. |
| `json` | One JSON object per line (NDJSON) on **stderr**. |
| `text` | A single carriage-return status line on **stderr**. |

## Streams and exit codes

- **stderr** is the progress channel. In `json` mode every line written by
  `winsuck` to stderr is a complete JSON object, including errors.
- **stdout** keeps the human-readable final summary (`files=… bytes=…`). Its
  format is not part of this contract; do not parse it.
- **Exit code** is `0` on success and `1` on failure. A failure always produces
  exactly one `error` event in `json` mode.

Callers must drain stderr continuously while the process runs. During `pull`
the Windows sender writes into a pipe; if the caller stops reading, the sender
can block.

## Event schema

Every event carries this shape. Optional fields are omitted when empty.

| Field | Type | Presence | Meaning |
| --- | --- | --- | --- |
| `v` | integer | always | Schema version. Currently `1`. |
| `phase` | string | always | `scanning`, `transferring`, `extracting`, `done`, or `error`. |
| `files_done` | integer | always | Files discovered or processed so far. |
| `files_total` | integer | when known | Total files in the transfer. |
| `bytes_done` | integer | always | Bytes discovered or processed so far. |
| `bytes_total` | integer | when known | Total bytes in the transfer. |
| `skipped` | integer | when `> 0` | Files skipped by `--update`. |
| `current` | string | when available | Relative path of the file being processed. |
| `elapsed_ms` | integer | always | Milliseconds since the command started. |
| `error` | string | on `error` | Failure message. |

Example:

```json
{"v":1,"phase":"scanning","files_done":1200,"bytes_done":52428800,"elapsed_ms":830}
{"v":1,"phase":"transferring","files_done":4200,"files_total":358000,"bytes_done":734003200,"bytes_total":21474836480,"current":"Models/Example.xpp","elapsed_ms":2400}
{"v":1,"phase":"done","files_done":358000,"files_total":358000,"bytes_done":21474836480,"bytes_total":21474836480,"skipped":12,"elapsed_ms":154000}
```

## Phases per command

| Command | Sequence |
| --- | --- |
| `send` | `scanning…` → `transferring…` → `done` |
| `listen` | `extracting…` → `done` (no totals; render an indeterminate bar) |
| `pull` | relayed from the Windows sender: `scanning…` → `transferring…` → `done` |
| `pull --dry-run` | `scanning…` → `done` |

`extracting` is emitted only by `listen`; `pull` does not forward the receiver
side to avoid duplicate progress.

## Totals

Totals are discovered while the Windows sender walks the source tree. Until the
walk completes, `files_total` and `bytes_total` are `0`/omitted, so:

- show an indeterminate status (spinner, "scanning…") while totals are unknown;
- switch to a percentage as soon as `files_total > 0`.

For status math prefer `files_done / files_total`. In `--update` mode the
denominator counts discovered files and `files_done` includes skipped files, so
the bar still reaches 100%.

## Terminal events

- `done` means the command succeeded. It is the only terminal success event.
- `error` means the command failed and is followed by exit code `1`. It is
  emitted once by the `winsuck` CLI; the in-process library reports failures
  through the returned `error` instead.

A consumer should finish when it sees `done` or `error`, whichever comes first.

## Throttling and guarantees

- Intermediate `scanning`, `transferring`, and `extracting` events are
  best-effort and throttled to roughly one per 100 ms. Small transfers may emit
  only one such event.
- `done` and `error` are always emitted.
- Events from a single transfer are serialized; no two lines interleave.

## Parsing rules

1. Read stderr line by line and JSON-decode each line independently.
2. Apply events in arrival order; the last terminal event wins.
3. Ignore unknown fields and unknown phases for forward compatibility.
4. Do not wait for a specific number of intermediate events; wait for a
   terminal event.
5. Check the exit code as the authoritative success signal.

## Examples

Bash:

```bash
winsuck pull "C:\src" ~/cache --progress=json 2> >(while read -r line; do
  echo "$line" | jq -r '"\(.phase): \(.files_done)/\(.files_total)"'
done)
```

Python:

```python
import json, subprocess

process = subprocess.Popen(
    ["winsuck", "pull", r"C:\src", "~/cache", "--progress=json"],
    stderr=subprocess.PIPE,
    text=True,
)
for line in process.stderr:
    event = json.loads(line)
    if event["phase"] in ("done", "error"):
        break
    total = event.get("files_total", 0)
    print(event["phase"], event["files_done"], total)
process.wait()
```

Go:

```go
command := exec.Command("winsuck", "pull", `C:\src`, home, "--progress=json")
stderr, _ := command.StderrPipe()
if err := command.Start(); err != nil {
	return err
}
scanner := bufio.NewScanner(stderr)
for scanner.Scan() {
	var event progress.Event
	if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
		continue
	}
	render(event)
}
return command.Wait()
```

## In-process usage

Go callers inside the winsuck module can subscribe directly:

```go
result, err := transfer.Send(transfer.SendOptions{
	Source:   `C:\src`,
	Address:  "127.0.0.1:9099",
	Progress: func(event progress.Event) { render(event) },
})
```

The events match the JSON schema. On failure the call returns an error and does
not emit an `error` event.

## Versioning

`v` identifies the schema. It is incremented only for changes that would break
an existing consumer; additive fields keep the current version.
