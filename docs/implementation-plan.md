# winsuck Implementation Plan

## Scope

`winsuck` transfers selected source files from Windows NTFS to a WSL ext4
destination. File payloads stream only from Windows to WSL as an uncompressed
TAR archive over loopback TCP. No temporary archive is created and WSL never
reads source files through `/mnt/c`.

## Commands and Configuration

- `winsuck pull` starts the Linux receiver and invokes `winsuck.exe send`.
- `winsuck listen` is the manual Linux receiver.
- `winsuck send` is the manual Windows producer.
- `--config winsuck.json` supplies source, destination, port, workers,
  include/exclude patterns, and update mode. Explicit scalar flags override
  config values; CLI include/exclude patterns are appended.
- TCP uses port `9099` by default but every command accepts `--port`.

## Transfer Protocol

1. The Linux receiver listens on `127.0.0.1`.
2. The Windows producer connects to it.
3. For update mode only, WSL sends compact manifest metadata before payload
   streaming. This is control data, not a reverse file transfer.
4. Windows sends a TAR stream directly to the TCP connection.
5. WSL extracts the TAR stream directly to the ext4 destination and writes an
   updated manifest only after a successful transfer.

## Performance Design

- Windows walks and opens source files natively.
- Traversal uses a bounded worker pool. A second reader pool opens and reads
  files concurrently, which hides per-file latency on Windows (for example
  antivirus scanning) and can be several times faster on trees with many small
  files. Files up to 8 MiB are buffered per reader; larger files fall back to a
  serial stream.
- TAR output remains serial so the stream stays valid and memory stays bounded.
- Include/exclude rules are compiled once and applied before opening files.
- In update mode, Windows still scans metadata but skips reading, archiving,
  and transmitting unchanged files.
- TAR payloads are intentionally not compressed.

## Safety and Validation

- TAR entries must be relative and cannot escape the destination.
- Extraction writes through temporary paths and atomically renames completed
  files.
- `.winsuckignore` in the source root adds exclude patterns.
- The receiver binds only to loopback by default.

## Delivery Sequence

1. Establish the Go module, command parsing, configuration, and filters.
2. Implement manual `listen` and `send` streaming.
3. Implement `pull` WSL orchestration.
4. Add manifest-backed update mode.
5. Add focused unit and integration tests, then benchmark representative trees.
