# winsuck Agent Notes

- This is a Go project. Use `go test ./...` for focused verification and cross-build with `GOOS=windows go build ./cmd/winsuck` or `GOOS=linux go build ./cmd/winsuck`.
- Preserve the core data path: traverse source files with native Windows I/O, write an uncompressed TAR stream over loopback TCP, and unpack directly into WSL's ext4 destination. Avoid per-file WSL `/mnt/c` reads and temporary archive files.
- `pull` is the WSL orchestrator: start the Linux listener, invoke `winsuck.exe send` through WSL interoperability, then receive and extract the stream. `listen` and `send` remain usable as manual receiver/sender commands.
- Keep the specified defaults unless intentionally changing the CLI contract: TCP port `9099`, worker count equal to CPU cores, no compression, and incremental sync disabled by default.
- Excludes and includes support CLI patterns; source-root `.winsuckignore` adds excludes. Update mode uses relative-path size and mtime state in `.winsuck-manifest.json`.
- `--config` loads a JSON project profile. CLI scalar flags override config values; CLI include/exclude values append to the profile's lists.
