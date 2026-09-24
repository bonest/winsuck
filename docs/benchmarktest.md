# Benchmark: D365 metadata cache creation

This document records the benchmark that motivated winsuck's parallel file
reader (v0.2.0). It compares the two engines used by `d365_context`'s
`bin/365-std-cache.sh create` to build the WSL metadata cache of a standard
D365 version.

## Scope

Build the WSL ext4 cache of a standard application version's metadata and
compare:

- `tar` engine: PowerShell discovery of `Ax*` directories plus a Windows
  `tar.exe` stream extracted by WSL `tar`.
- `winsuck` engine: `winsuck pull`, which streams an uncompressed TAR over
  loopback TCP and extracts directly.

## Environment

- WSL2 on Windows, Linux `x86_64`, Homebrew.
- Source: Windows NTFS `C:\Users\...\Dynamics365\10.0.2527.109\PackagesLocalDirectory`.
- Destination: WSL ext4 `~/std-cache/ax365/10.0.2527.109/PackagesLocalDirectory`.
- Dataset: **282,799 files, 2,663,167,523 bytes (2.66 GB)**.
- File selection (identical for both engines):
  `PackagesLocalDirectory/**/Ax*/**` and `PackagesLocalDirectory/**/*.labels`,
  excluding `PackagesLocalDirectory/**/AxLabelFile`.
- winsuck default worker count equals the Windows CPU count (22 in this run).

## Method

- `d365_context/bin/365-std-cache-benchmark.sh` rebuilds the cache once per run
  and engine and diffs the resulting inventories
  (`find -printf '%P\t%s\n' | sort`).
- A full `tar` run is very slow, so the `tar` rate was sampled over 120 seconds
  after discovery.
- winsuck 0.2.0 was run to completion.
- Warm vs cold refers to the Windows file cache; runs were repeated.

## Results

| Engine | Wall time | Rate | Files/bytes |
| --- | --- | --- | --- |
| `tar` (sampled 120 s) | ~45 min projected | ~104 files/s | 282,799 / 2.66 GB |
| winsuck 0.1.0 (cold, 180 s cap) | — | ~116 files/s | 20,878 partial |
| winsuck 0.1.0 (warm, 90 s cap) | — | ~282 files/s | 25,416 partial |
| **winsuck 0.2.0 (full, cold)** | **4 m 31 s** | **~1,043 files/s** | 282,799 / 2.66 GB |
| **winsuck 0.2.0 (full, warm)** | **1 m 49 s** | **~2,594 files/s** | 282,799 / 2.66 GB |

winsuck 0.2.0 produced exactly the same file count and byte total as the
winsuck dry-run (`282,799` / `2,663,167,523`), and the benchmark's inventory
diff reported identical trees.

## Micro-benchmarks

These isolate where the time goes on the same dataset.

| Test | Result |
| --- | --- |
| Read 500 metadata files sequentially on Windows (cold) | 7.82 s (~64 files/s) |
| Read 500 metadata files sequentially on Windows (warm) | 1.32 s (~380 files/s) |
| winsuck Linux -> Linux loopback, 10,000 files | 0.40 s (25,000 files/s) |
| winsuck Windows -> Linux, 10,000 synthetic files | 5.2 s (~1,900 files/s) |
| winsuck single 512 MiB file | 10.4 s (~51 MB/s) |
| 4 concurrent winsuck 0.1.0 instances | 1,306 files/s aggregate (~326 files/s each) |

## Analysis

- The Linux-side pipeline is not the bottleneck: 10,000 files over loopback
  move in 0.4 s.
- A single 512 MiB file transfers at ~51 MB/s, so raw bandwidth is adequate.
- Reading the real metadata tree on Windows is dominated by **per-file
  latency** (~15 ms cold, ~2.6 ms warm per file), consistent with antivirus
  real-time scanning. The average file is only ~9 KB.
- Four concurrent processes scaled to ~1,300 files/s versus ~282 files/s for a
  single process, proving the workload is latency-bound rather than
  bandwidth- or CPU-bound.
- winsuck 0.1.0 read files serially, so it inherited the full per-file latency.
  winsuck 0.2.0 adds a parallel reader pool that hides it, giving roughly a
  10x improvement over the `tar` engine and ~3.7x over winsuck 0.1.0.

## Reproduce

```bash
# From d365_context:
./bin/365-std-cache-benchmark.sh 10.0.2527.109 --runs 2 --output-format text

# Direct winsuck comparison:
VER=10.0.2527.109
WIN="$(wslpath -w "/mnt/c/Users/.../Dynamics365/$VER")"
time winsuck pull "$WIN" "$HOME/std-cache/ax365/$VER" \
  --include 'PackagesLocalDirectory/**/Ax*/**' \
  --include 'PackagesLocalDirectory/**/*.labels' \
  --exclude 'PackagesLocalDirectory/**/AxLabelFile'
```

## Conclusion

For trees with many small files, per-file latency on the Windows side dominates
cache creation. winsuck 0.2.0's parallel reader turns this into a ~10x speedup
over the `tar` engine. `d365_context` can opt in with
`365-std-cache.sh create <version> --engine winsuck`.
