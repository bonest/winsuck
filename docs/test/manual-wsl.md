# Manual WSL Test

This guide verifies a Windows NTFS to WSL ext4 transfer using a small fixture
tree before running against a large source tree.

## Prerequisites

- Run the Linux commands in WSL.
- Ensure WSL interoperability is enabled so WSL can execute `.exe` files.
- Build both binaries into the repository root:

```bash
GOOS=linux go build -o winsuck ./cmd/winsuck
GOOS=windows go build -o winsuck.exe ./cmd/winsuck
```

`pull` locates `winsuck.exe` next to the Linux `winsuck` binary. No Windows
installation, Go runtime, service, or temporary archive directory is needed.

## Create Windows Fixture Data

Run the following in Windows PowerShell:

```powershell
New-Item -ItemType Directory -Force C:\temp\winsuck-source\Models\Example
New-Item -ItemType Directory -Force C:\temp\winsuck-source\Models\Example\bin
Set-Content C:\temp\winsuck-source\Models\Example\Example.xpp "class Example {}"
Set-Content C:\temp\winsuck-source\Models\Example\metadata.xml "<model />"
Set-Content C:\temp\winsuck-source\Models\Example\bin\generated.dll "skip"
Set-Content C:\temp\winsuck-source\README.txt "skip when include rules apply"
```

## Run a Basic Transfer

From WSL, run:

```bash
rm -rf /tmp/winsuck-destination
./winsuck pull 'C:\temp\winsuck-source' /tmp/winsuck-destination \
  --exclude '**/bin/**' --exclude '**/*.dll'
```

Verify the ext4 output from WSL:

```bash
find /tmp/winsuck-destination -type f | sort
cat /tmp/winsuck-destination/Models/Example/Example.xpp
test ! -e /tmp/winsuck-destination/Models/Example/bin/generated.dll && echo "exclude works"
```

## Run with a Project Profile

Use the example profile in this directory as a starting point. Update its
`source` and `destination` fields, then run:

```bash
./winsuck pull --config docs/test/winsuck.example.json
```

The profile includes only X++ and XML files below `Models`, and excludes build
outputs. Explicit scalar CLI flags override profile fields. CLI `--include` and
`--exclude` patterns append to profile lists.

## Verify Incremental Mode

Run the configured transfer twice with update mode enabled:

```bash
./winsuck pull --config docs/test/winsuck.example.json --update
./winsuck pull --config docs/test/winsuck.example.json --update
```

The first run creates `.winsuck-manifest.json` in the destination. The second
run should report zero transferred files and skipped unchanged files. Change
`Example.xpp` in Windows PowerShell and run it once more; only the changed file
should transfer.

## Manual Sender/Receiver Test

Use this only when diagnosing orchestration or WSL interoperability issues.

Start the receiver in WSL:

```bash
./winsuck listen --dest /tmp/winsuck-manual --port 9099
```

Then start the producer in Windows PowerShell from the directory containing
`winsuck.exe`:

```powershell
.\winsuck.exe send --src C:\temp\winsuck-source --host 127.0.0.1 --port 9099
```

If the sender cannot connect, first confirm both commands use the same port and
that WSL localhost forwarding/interoperability has not been disabled.
