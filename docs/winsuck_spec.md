# Projektbeskrivelse: winsuck

> **winsuck** – Et lynhurtigt *Windows-to-WSL Data Ingestion Pipeline* værktøj til at suge massive filtræer (f.eks. Dynamics 365 FO kildekode) fra Windows NTFS ind i WSL2 ext4.

---

## 1. Problemstilling og Baggrund

### 1.1 Flaskehalsen ved 9P i WSL2
WSL2 afvikles som en virtuel maskine oven på Hyper-V. Når Linux-miljøet i WSL2 tilgår Windows-drev via automounten `/mnt/c/`, sker det gennem netværksprotokollen **9P (Plan 9)**.
* Hvert enkelt filopslag, `stat()`, `open()`, og `read()` medfører en netværks-RPC på tværs af VM-grænsen.
* For kodebaser som **Microsoft Dynamics 365 Finance & Operations (`PackagesLocalDirectory`)** bestående af 20+ GB fordelt over 300.000–600.000 små XML-, XPP-, DLL- og ressourcefiler, resulterer dette i ekstremt lav I/O-hastighed.
* Standardværktøjer som `cp`, `rsync` eller `find` fra WSL mod `/mnt/c` tager timer eller går i stå.

### 1.2 Formål med winsuck
Formålet med `winsuck` er at **suge** filer fra Windows NTFS og placere dem i det native Linux ext4-filsystem (f.eks. `~/std-cache/packages`), så AI-agenter, sprogservere (LSP), indekseringsværktøjer (`ripgrep`, `ast-grep`) og søgeagenter kan læse dem med native ext4 NVMe-hastighed.

---

## 2. Arkitektur og Hovedprincipper

`winsuck` omgår 9P-flaskehalsen ved at flytte filscanningen til Windows-værten og overføre data som én uafbrudt, sekventiel bytestrøm over en lokal loopback-forbindelse.

```
+-------------------------------------------------------------------------+
| WINDOWS HOST (NTFS)                                                     |
|                                                                         |
|  [C:\AOSService\PackagesLocalDirectory]                               |
|            │                                                            |
|            ▼ (Hurtig native NTFS traversal via parallelle tråde)        |
|  [ winsuck.exe (Producer) ]                                             |
|            │                                                            |
|            ▼ (Ukomprimeret TAR-stream via TCP socket 127.0.0.1:9099)    |
+------------┼------------------------------------------------------------+
             │  (Loopback IP / Hyper-V Virtual Socket)
+------------┼------------------------------------------------------------+
| WSL2 VIRTUAL MACHINE (Linux ext4)                                       |
|            │                                                            |
|            ▼                                                            |
|  [ winsuck (Consumer / Orchestrator) ]                                  |
|            │                                                            |
|            ▼ (Streaming unpack direkte i RAM/FS buffer)                 |
|  [ ~/std-cache/packages ]                                               |
+-------------------------------------------------------------------------+
```

### 2.1 Kerneprincipper
1. **Host-native I/O:** Filscanning og læsning sker udelukkende med Windows-native Win32 API'er / Go/Rust standardbibliotek for at udnytte Windows' OS disk-cache.
2. **Én enkelt strøm (Ingen pr.-fil overhead):** Filer indkapsles i en `tar`-stream on-the-fly. Hverken afsender eller modtager opretter midlertidige arkivfiler på disken.
3. **Ingen CPU-komprimering som standard:** Da overførslen sker over lokal RAM-loopback (`127.0.0.1`), er CPU-komprimering (`gzip`, `zstd`) en unødig flaskehals. Rå I/O er hurtigst.
4. **Gennemsigtig orkestrering:** Brugeren kalder blot `winsuck pull /mnt/c/... ~/cache` inde fra WSL. Linux-binæren starter automatisk Windows-modparten (`winsuck.exe`) via WSL interoperabilitet.

---

## 3. Teknisk Specifikation

### 3.1 Teknologivalg: Go eller Rust
Projektet designes til at kunne implementeres i enten **Go** eller **Rust**:
* **Go (Anbefalet til hurtig udvikling):**
  * Standardbiblioteket indeholder `archive/tar`, `net`, `filepath`.
  * Krydskompilering klares problemfrit (`GOOS=windows go build` og `GOOS=linux go build`).
  * Letvægts goroutines til parallel directory traversal.
* **Rust (Alternativ til maksimal throughput):**
  * Biblioteker: `jwalk` (multi-threaded directory walk), `tokio` (asynkron netværk/IO), `tar`.
  * Højeste mulige throughput og laveste hukommelsesforbrug.

### 3.2 CLI Brugerflade
CLI'et skal være intuitivt og understøtte både automatiseret og manuel brug:

```bash
# Fra WSL (anbefalet standardbrug):
winsuck pull "C:\AOSService\PackagesLocalDirectory" ~/std-cache/fo-metadata

# Med filtre (udelad binære filer og build output):
winsuck pull "C:\AOSService\PackagesLocalDirectory" ~/std-cache/fo-metadata \
    --exclude "*.dll" --exclude "*.pdb" --exclude "bin/*" --exclude "Resources/*"

# Differentiel / Inkrementel synkronisering:
winsuck pull "C:\AOSService\PackagesLocalDirectory" ~/std-cache/fo-metadata --update

# Manuel server/klient tilstand (hvis interoperabilitet er deaktiveret):
# På Linux (modtager):
winsuck listen --dest ~/std-cache/fo-metadata --port 9099
# På Windows (afsender):
winsuck.exe send --src "C:\AOSService\PackagesLocalDirectory" --host 127.0.0.1 --port 9099
```

### 3.3 CLI Parametre og Konfiguration
| Flag | Beskrivelse | Default |
| :--- | :--- | :--- |
| `pull` | Overordnet orkestreringskommando (køres i WSL) | - |
| `--src` | Windows kildesti (støtter `C:\...` eller `/mnt/c/...`) | Påkrævet |
| `--dest` | WSL destinationsmappe | Påkrævet |
| `--exclude` | Glob-mønstre til ekskludering af filer/mapper | `[]` |
| `--workers` | Antal parallelle disk-scannere / tråde | Antal CPU-kerner |
| `--port` | TCP port til loopback overførsel | `9099` |
| `--update` | Inkrementel synkronisering (kun ændrede `mtime`/størrelse) | `false` |
| `--dry-run` | Viser antal filer og estimeret størrelse uden at overføre | `false` |

---

## 4. Kravspecifikation for Agenter (Prompt Tasks)

Følgende opgaver er modulære instruktioner, som en udvikler eller en AI-kodeagent kan udføre trin for trin.

### Opgave 1: Core Stream Engine (Producer & Consumer)
* Implementer `tar`-streaming over en rå `net.Conn` (TCP).
* **Producer (Windows):** Gennemløb `src`-mappen rekursivt, generer tar-headere med relative stier, og skriv rå fildata til socketten.
* **Consumer (Linux):** Modtag data fra socketten, læs tar-headere, opret mapper med `mkdirAll`, og skriv filindhold til destinationen.

### Opgave 2: Automatisk WSL-orkestrering
* Når brugeren kalder `winsuck pull <src> <dest>` fra Linux:
  1. Kontroller om `winsuck.exe` ligger i samme sti eller på Windows PATH.
  2. Åbn en lokal TCP-lytter i baggrunden på Linux.
  3. Kald `winsuck.exe send --src <src> --addr 127.0.0.1:<port>` via WSL subprocess/exec.
  4. Modtag og pak filerne ud med realtids statusindikator (antal filer og bytes overført).

### Opgave 3: Filter- og Ekskluderingsmotor
* Tilføj understøttelse af `--exclude` med glob-matching (`doublestar` eller regex).
* Understøt læsning af en `.winsuckignore`-fil i roden af kildemappen.
* Typisk standardprofil for D365FO:
  ```gitignore
  **/bin/**
  **/*.dll
  **/*.pdb
  **/*.cache
  **/Resources/**
  ```

### Opgave 4: Inkrementel / Differentiel Cache Mode
* For at undgå at gensuge 20 GB hver dag:
  1. Consumer genererer en letvægts tilstandsfil (`.winsuck-manifest.json`) indeholdende `{rel_path: {size, mtime_unix}}`.
  2. Ved start sender Consumer dette manifest (eller et hash-map) til Producer.
  3. Producer springer filer over, hvis `size` og `mtime` matcher.
  4. Kun nye eller ændrede filer pakkes i streamen.

---

## 5. Performance Målsætning og Acceptkriterier

* **Test-datasæt:** D365FO `PackagesLocalDirectory` (~20 GB, >350.000 filer).
* **Standard 9P `cp -r` / `rsync`:** > 2-5 timer (eller crasher).
* **Målsætning for `winsuck`:**
  * Første fulde træk (Initial suck): **Under 2-4 minutter** (begrænset af NVMe-sekventiel skrivehastighed på ext4).
  * Inkrementel opdatering (Incremental suck): **Under 15-30 sekunder** for scanning og synkronisering af ændringer.
