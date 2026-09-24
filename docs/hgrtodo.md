# Udgiv winsuck til WSL og Windows

Denne guide beskriver en distributionsmodel, hvor en WSL-bruger installerer
`winsuck` med Homebrew, og hvor den nødvendige `winsuck.exe` installeres ved
siden af Linux-binæren. Derfor kan `winsuck pull` starte Windows-afsenderen
automatisk gennem WSL interoperability. En separat PowerShell-installation er
kun nødvendig for brugere, der vil køre `winsuck.exe send` direkte fra Windows.

Eksemplerne bruger GitHub-kontoen `bonest`. Udskift den, repositorynavne og
versioner med de faktiske værdier.

> **Status:** Release-pipelinen er sat i drift. `v0.1.0` er publiceret på
> GitHub med `winsuck_0.1.0_linux_amd64.tar.gz`, `winsuck_0.1.0_windows_amd64.zip`
> og `checksums.txt`, og `bonest/homebrew-tap` indeholder `Formula/winsuck.rb`.
> `brew install winsuck` er verificeret i WSL. Afsnittene nedenfor er den
> generelle opskrift og kan genbruges ved næste release.

## Før sektion 1: den korte sammenhæng

Der er tre niveauer, som er vigtige ikke at blande sammen:

| Niveau | Hvad det er | Eksempel |
| --- | --- | --- |
| Kildekode | Go-filerne du redigerer. De kan ikke køres direkte. | `cmd/winsuck/main.go` |
| Lokal build | En binær du bygger på din WSL-maskine for at teste ændringer. | `./winsuck` |
| Release | Versionsmærkede binærer, bygget én gang og publiceret til brugere. | GitHub Release `v0.1.0` |

Når du ændrer Go-kode, opdateres ingen binær automatisk. Du skal bygge igen.
Når du skriver `./winsuck`, kører du binæren i den aktuelle mappe. Når du
skriver `winsuck` uden `./`, finder shellen en tidligere installeret binær via
`PATH`, eksempelvis en Homebrew-version. Derfor kan en gammel binær sige
`unknown command "--version"`, selv om den nye kildekode understøtter flaget.

Den overordnede kæde er:

```text
Go-kildekode
  -> go build (lokal testbinær)
  -> Git-tag, fx v0.1.0
  -> GitHub Actions + GoReleaser (release-binærer og arkiver)
  -> GitHub Release (downloads og checksums)
  -> Homebrew (installerer de publicerede binærer)
```

## 1. Fastlæg hvad en release er

En **release-kontrakt** er ikke en juridisk kontrakt eller et særligt
Go-koncept. Det er blot en fast aftale om, hvilke downloadfiler hver version af
winsuck leverer, hvad de hedder, og hvilke platforme de virker på.

Den er nødvendig, fordi Homebrew ikke bygger winsuck fra kildekoden på brugerens
maskine. Homebrew downloader en konkret fil fra en GitHub Release og skal kende
filens URL og SHA-256 checksum på forhånd.

Tænk på sektion 1 som designet af produktets leveringskasse: før vi automatiserer
samlebåndet, beslutter vi hvilke kasser der skal leveres, hvad der er i dem, og
hvordan installatøren identificerer dem. Du behøver ikke oprette eller uploade
nogen filer i sektion 1.

### Begreber

- **Binær:** Det færdige program. Linux-binæren hedder `winsuck`; Windows-
  binæren hedder `winsuck.exe`.
- **Arkiv:** En indpakning omkring en binær. Linux bruger normalt `tar.gz` og
  Windows bruger normalt `zip`.
- **Git-tag:** Et navn på én bestemt commit. `v0.1.0` betyder, at den commit er
  winsuck version 0.1.0.
- **GitHub Release:** GitHubs downloadside for et tag. Filerne på siden kaldes
  release assets.
- **SHA-256 checksum:** Et kryptografisk fingeraftryk af en fil. Hvis én byte
  ændres, ændres checksummen. Homebrew afviser filer med forkert checksum.

### Den konkrete aftale for winsuck

Første version understøtter 64-bit Intel/AMD-maskiner:

| Platform | Program i arkivet | Arkivformat | Formål |
| --- | --- | --- | --- |
| WSL/Linux amd64 | `winsuck` | `tar.gz` | Programmet agents kører i WSL. |
| Windows amd64 | `winsuck.exe` | `zip` | Programmet læser Windows-filer native. |

En GitHub Release med tagget `v0.1.0` skal ende med disse tre downloadfiler:

```text
winsuck_0.1.0_linux_amd64.tar.gz
winsuck_0.1.0_windows_amd64.zip
checksums.txt
```

`checksums.txt` indeholder én linje pr. arkiv, eksempelvis:

```text
f4...a9  winsuck_0.1.0_linux_amd64.tar.gz
9c...e1  winsuck_0.1.0_windows_amd64.zip
```

### Skal du oprette pakkerne nu?

**Nej.** Sektion 1 fastlægger kun målet. Du skal ikke manuelt køre `tar`, `zip`
eller kopiere binærer rundt i dette afsnit.

Pakkerne oprettes senere af GoReleaser:

1. I sektion 4 kører du en lokal test-release:

```bash
goreleaser release --snapshot --clean
```

Den bygger test-arkiver under den lokale `dist/`-mappe. De uploades ikke og må
ikke committes.

2. I sektion 5 pusher du et rigtigt Git-tag:

```bash
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

GitHub Actions starter automatisk GoReleaser. Den bygger de samme to arkiver,
beregner checksums og uploader dem til GitHub Release-siden. Det er disse
publicerede arkiver, Homebrew og PowerShell-installeren senere bruger.

En GitHub Release er altså ikke det samme som en GitHub commit. En commit er
kildekodehistorik. En release er en navngivet version af koden plus færdige
downloads. GitHub beskriver begrebet her:
<https://docs.github.com/repositories/releasing-projects-on-github/about-releases>.

### Hvad du gør nu

1. Kontrollér at din WSL-maskine er amd64:

```bash
uname -m
```

Forventet output er `x86_64`. Hvis du ser `aarch64`, skal Linux arm64 tilføjes
som en senere, separat buildvariant.

2. Brug versionsformatet `vMAJOR.MINOR.PATCH`: `v0.1.0` for første offentlige
release, `v0.1.1` for en rettelse og `v0.2.0` for ny funktionalitet.
3. Genbrug aldrig et publiceret tag. Hvis `v0.1.0` har en fejl, opretter du
`v0.1.1`; du erstatter ikke filerne i `v0.1.0`. Homebrew forventer, at en
versions download altid har samme checksum.
4. Fortsæt til sektion 2. Den første kommando, der faktisk opretter pakker,
er `goreleaser release --snapshot --clean` i sektion 4.

Homebrew installerer begge binærer i samme `bin`-mappe. `pull` finder først
`winsuck.exe` ved siden af `winsuck`, så Windows-binæren behøver normalt ikke
være på Windows `PATH`.

## 2. Forstå build og version i CLI'en

Denne sektion er allerede implementeret i winsuck. Formålet er at forstå, hvad
der sker når du bygger, hvorfor du ser `dev`, og hvordan en officiel release
senere får et rigtigt versionsnummer.

### Hvad `go build` gør

Go compilerer kildekoden direkte til én selvstændig binær. Du behøver ikke en
Go-runtime på brugerens maskine, og du behøver ikke først oprette en traditionel
"build folder". Kommandoen læser `go.mod` for modulnavn og Go-version,
compilerer pakken `./cmd/winsuck` og skriver den færdige binær til stien efter
`-o`:

```bash
go build -o ./winsuck ./cmd/winsuck
```

| Del | Betydning |
| --- | --- |
| `go build` | Compilér Go-koden. |
| `-o ./winsuck` | Skriv resultatet som filen `winsuck` i den aktuelle mappe. |
| `./cmd/winsuck` | Byg command-pakken med `package main`, altså CLI-programmet. |

Da kommandoen køres i WSL, er standardmålet Linux for din egen CPU-arkitektur.
Derfor kan den oprettede `./winsuck` køres direkte i WSL:

```bash
./winsuck --version
```

Den viser `dev` efter en normal lokal build. Det betyder ikke, at noget er
forkert; det betyder "denne binær er bygget direkte fra udviklingskoden og har
ikke fået et release-versionsnummer".

Go kan også cross-kompilere. Denne kommando bygger samme kildekode som en
Windows-binær uden at installere Go på Windows:

```bash
GOOS=windows GOARCH=amd64 go build -o ./winsuck.exe ./cmd/winsuck
```

`GOOS=windows` vælger Windows som target. `GOARCH=amd64` vælger 64-bit
Intel/AMD. `winsuck.exe` køres af Windows, eller automatisk fra WSL gennem WSL
interoperability; den er ikke en almindelig Linux-binær.

Den officielle introduktion til Go-builds er:
<https://go.dev/doc/tutorial/compile-install>. Den fulde reference for
`go build` er: <https://pkg.go.dev/cmd/go#hdr-Compile_packages_and_dependencies>.

### Hvordan winsuck får sin version

I `cmd/winsuck/main.go` findes denne variabel:

```go
var version = "dev"
```

`winsuck --version` skriver værdien og afslutter uden at forsøge at starte
`pull`, `listen` eller `send`.

Ved en lokal `go build` ændrer ingen noget i variablen, så binæren indeholder
teksten `dev`. Ved en release kan Go-linkeren erstatte variablens værdi, mens
den færdige binær bliver samlet:

```bash
-ldflags "-s -w -X main.version={{ .Version }}"
```

| Del | Betydning |
| --- | --- |
| `-ldflags` | Send flags videre til Go-linkeren under build. |
| `-s -w` | Fjern symbol- og debugdata for en mindre release-binær. |
| `-X main.version=...` | Erstat tekstvariablen `version` i `main`-pakken. |
| `{{ .Version }}` | En GoReleaser-pladsholder, som bliver til versionen fra Git-tagget. |

Det er ikke en runtime-konfiguration og ikke en fil, winsuck læser ved start.
Versionsstrengen bygges permanent ind i den enkelte binær. To builds med
forskellige `-X main.version=...` er derfor to forskellige binærfiler.

### Prøv det selv før release

Kør disse kommandoer fra repositoryroden. De ændrer ikke kildekoden:

```bash
# En almindelig udviklingsbuild.
go build -o /tmp/winsuck-dev ./cmd/winsuck
/tmp/winsuck-dev --version

# En test-release-build med en manuelt valgt version.
go build -ldflags "-X main.version=0.1.0-test" -o /tmp/winsuck ./cmd/winsuck
/tmp/winsuck --version
```

Forventet output er først `dev` og derefter `0.1.0-test`. Det beviser, at den
kommende GoReleaser-konfiguration kan sætte release-versionen uden at kode skal
ændres mellem hver release.

Når du senere pusher Git-tagget `v0.1.0`, vil GoReleaser udføre samme type build
for dig og sætte versionen til `0.1.0`. Release-binærens output bliver derfor:

```text
0.1.0
```

CLI'en understøtter også `winsuck --help`, `winsuck help <command>` og
`winsuck <command> --help`. Brug `winsuck --help` til at se alle muligheder.

Læs om versionsformatet SemVer her: <https://semver.org/lang/da/>. Du behøver
ikke forstå alle detaljer nu; brug blot `v0.1.0` som første tag og lad
GoReleaser bruge det til binærens version.

## 3. Installér GoReleaser lokalt

GoReleaser opretter cross-platform binærer, arkiver og checksums fra et Git-tag.
På WSL med Homebrew:

```bash
brew install goreleaser
```

Alternativt kan den officielle GoReleaser-installationsmetode bruges. Kontrollér
installationen:

```bash
goreleaser --version
```

## 4. Tilføj `.goreleaser.yaml`

Opret filen i projektroden, dvs. samme mappe som `go.mod`:

```bash
touch .goreleaser.yaml
```

Indsæt denne konfiguration. YAML bruger indrykning som syntaks, så brug mellemrum
og bevar indrykningen præcist:

```yaml
version: 2
project_name: winsuck

before:
  hooks:
    - go test ./...

builds:
  - id: winsuck
    main: ./cmd/winsuck
    binary: winsuck
    goos: [linux, windows]
    goarch: [amd64]
    ldflags:
      - -s -w -X main.version={{ .Version }}

archives:
  - id: winsuck
    ids: [winsuck]
    formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    format_overrides:
      - goos: windows
        formats: [zip]

checksum:
  name_template: checksums.txt
```

Hvad de vigtigste felter betyder:

- `builds` bygger samme Go-program for både Linux og Windows fra én
  build-definition.
- `archives` pakker binærerne i `tar.gz` som standard; `format_overrides` gør
  Windows-arkivet til en `zip`, som kan åbnes direkte i Windows.
- `ldflags` indsætter Git-taggets version i `main.version` uden at ændre
  kildekoden.
- `checksum` producerer hashes, som Homebrew og PowerShell-installeren bruger
  til at opdage korrupte eller manipulerede downloads.

Tilføj derefter følgende til `.gitignore`, hvis den ikke allerede er der:

```text
/dist/
```

Kør en lokal snapshot-release. `--snapshot` betyder "byg som en test" og
opretter ikke en GitHub Release. `--clean` sletter en gammel `dist/` først:

```bash
goreleaser release --snapshot --clean
ls dist/
```

Kontrollér resultatet:

```bash
find dist -maxdepth 2 -type f | sort
cat dist/checksums.txt
```

Du skal se Linux-arkivet, Windows-arkivet og `checksums.txt`. `dist/` er
genereret output og må aldrig committes.

## 5. Opret GitHub release-workflow

Opret `.github/workflows/release.yml`. Workflowet udgiver kun, når et tag med
prefixet `v` pushes:

```yaml
name: Release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
      - run: go test ./...
      - run: go vet ./...
      - uses: goreleaser/goreleaser-action@v7
        with:
          distribution: goreleaser
          version: "~> v2"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

Commit først version-CLI'en, `.goreleaser.yaml`, `.gitignore`-ændringen og
workflowet. Kontrollér før commit, at `dist/` ikke er på listen:

```bash
git status
git add -A
git commit -m "Add release automation"
git push origin "$(git branch --show-current)"
```

Verificér derefter hele release-processen med et pre-release-tag. Et Git-tag er
en permanent markering af præcis den commit, der skal udgives:

```bash
git tag v0.1.0-rc.1
git push origin v0.1.0-rc.1
```

Åbn derefter repositoryets fane **Actions** i GitHub. Vælg workflowet
"Release" og vent på et grønt flueben. Åbn så **Releases**. Der skal være en
release med tre assets: begge arkiver og `checksums.txt`. Download dem manuelt
én gang og kontrollér, at Windows-arkivet indeholder `winsuck.exe`.

Hvis workflowet fejler med en fejl om GitHub-token-rettigheder, kontrollér i
repositoryets **Settings -> Actions -> General**, at standardrettighederne ikke
forbyder workflowet at skrive releases. `permissions: contents: write` i
workflowet er den specifikke rettighed, release-jobbet kræver.

## 6. Opret Homebrew tap

En tap er blot et separat GitHub-repository, som Homebrew kan klone. Opret et
nyt **public** repository i GitHub-webinterfacet med præcis dette navn:

```text
bonest/homebrew-tap
```

Klon derefter det tomme tap-repository i en anden mappe end winsuck-kilden:

```bash
cd ~/source/github/bonest
git clone git@github.com:bonest/homebrew-tap.git
cd homebrew-tap
mkdir -p Formula
touch Formula/winsuck.rb
```

Hvis du bruger HTTPS fremfor SSH på GitHub, brug HTTPS-klone-URL'en. Homebrew
forventer formulaer under `Formula/`:

```text
homebrew-tap/
  Formula/
    winsuck.rb
```

Brug følgende formula som startpunkt. Indsæt den rigtige version og SHA-256
værdierne fra den publicerede `checksums.txt`. En SHA-256-værdi er den første
kolonne på den linje, der slutter med det relevante filnavn:

```bash
# Kør fra en downloadet release eller kopier indholdet fra GitHub Release-assetet.
cat checksums.txt
```

```ruby
class Winsuck < Formula
  desc "Stream selected Windows NTFS files into WSL ext4"
  homepage "https://github.com/bonest/winsuck"
  license "MIT"

  on_linux do
    url "https://github.com/bonest/winsuck/releases/download/v0.1.0/winsuck_0.1.0_linux_amd64.tar.gz"
    sha256 "LINUX_ARCHIVE_SHA256"

    resource "windows-sender" do
      url "https://github.com/bonest/winsuck/releases/download/v0.1.0/winsuck_0.1.0_windows_amd64.zip"
      sha256 "WINDOWS_ARCHIVE_SHA256"
    end
  end

  def install
    odie "winsuck supports WSL/Linux only" unless OS.linux?

    bin.install "winsuck"
    resource("windows-sender").stage do
      bin.install "winsuck.exe"
    end
  end

  test do
    system "#{bin}/winsuck", "--version"
  end
end
```

Commit og push formulaen:

```bash
git add Formula/winsuck.rb
git commit -m "Add winsuck formula"
git push origin main
```

Fremtidige versionsopdateringer ændrer kun version, to download-URL'er og to
checksums i formulaen. Før første push kan formulaen testes fra dens lokale
mappe:

```bash
brew install --build-from-source ./Formula/winsuck.rb
brew test winsuck
brew uninstall winsuck
```

Kør altid:

```bash
brew audit --strict bonest/tap/winsuck
```

før formulaen annonceres.

## 7. Test Homebrew-installationen i WSL

Test helst i en ren WSL-distribution eller efter at have fjernet lokale builds
fra repositoryroden, så testen ikke utilsigtet bruger dem. Start med at se,
hvilken binær shellen ellers ville vælge:

```bash
command -v winsuck || true
echo "$PATH"
```

Installer derefter fra tap'en:

```bash
brew tap bonest/tap
brew trust bonest/tap
brew install winsuck
which winsuck
winsuck --version
ls -l "$(dirname "$(command -v winsuck)")/winsuck.exe"
```

Homebrew 7 afviser utrustede tredjeparts-taps, så `brew trust bonest/tap` er
nødvendig før installation.

`which winsuck` skal pege ind under din Homebrew-prefix, typisk
`/home/linuxbrew/.linuxbrew/bin/winsuck`. `ls`-kommandoen skal vise en
`winsuck.exe` i præcis samme mappe.

Test derefter en lille, reel transfer. Følg først fixture-delen i
[`manual-wsl.md`](test/manual-wsl.md), hvis `C:\temp\winsuck-source` ikke
allerede findes:

```bash
winsuck pull 'C:\temp\winsuck-source' /tmp/winsuck-homebrew-test
find /tmp/winsuck-homebrew-test -type f
```

Hvis `pull` ikke kan starte Windows-binæren, kontrollér først at companion-filen
findes ved siden af `winsuck` som vist ovenfor. Kontrollér derefter at WSL
interoperability er slået til. Afprøv det direkte i WSL:

```bash
"$(dirname "$(command -v winsuck)")/winsuck.exe" --version
```

Hvis den kommando ikke kan starte en `.exe`, er problemet WSL
interoperability og ikke Homebrew eller winsuck. Kontrollér `/etc/wsl.conf` for
en deaktiveret `[interop]`-sektion, og genstart WSL fra Windows med:

```powershell
wsl --shutdown
```

## 8. Valgfri PowerShell-installer

Denne del er kun påkrævet for manuel brug fra Windows PowerShell.
`scripts/install.ps1` findes nu i winsuck-repositoryet. Scriptet henter et
Windows release-arkiv, verificerer checksum, installerer per bruger og opdaterer
brugerens `PATH` uden administratorrettigheder.

Indholdet er:

```powershell
param(
  [string]$Version = "latest"
)

$Repository = "bonest/winsuck"
$InstallDirectory = Join-Path $env:LOCALAPPDATA "winsuck\bin"
$ApiUrl = "https://api.github.com/repos/$Repository/releases"

if ($Version -eq "latest") {
  $Release = Invoke-RestMethod "$ApiUrl/latest"
} else {
  $Release = Invoke-RestMethod "$ApiUrl/tags/v$Version"
}

$Archive = $Release.assets | Where-Object {
  $_.name -match "^winsuck_.*_windows_amd64\.zip$"
} | Select-Object -First 1
$Checksums = $Release.assets | Where-Object { $_.name -eq "checksums.txt" } |
  Select-Object -First 1

if (-not $Archive -or -not $Checksums) {
  throw "Windows archive or checksums.txt is missing from the release."
}

$TemporaryDirectory = Join-Path ([System.IO.Path]::GetTempPath()) "winsuck-install"
New-Item -ItemType Directory -Force $TemporaryDirectory, $InstallDirectory | Out-Null
$ArchivePath = Join-Path $TemporaryDirectory $Archive.name
$ChecksumsPath = Join-Path $TemporaryDirectory "checksums.txt"

Invoke-WebRequest $Archive.browser_download_url -OutFile $ArchivePath
Invoke-WebRequest $Checksums.browser_download_url -OutFile $ChecksumsPath

$Expected = ((Select-String -Path $ChecksumsPath -Pattern ([regex]::Escape($Archive.name))).Line -split '\s+')[0].ToLower()
$Actual = (Get-FileHash -Algorithm SHA256 $ArchivePath).Hash.ToLower()
if ($Expected -ne $Actual) {
  throw "Checksum verification failed for $($Archive.name)."
}

Expand-Archive -Path $ArchivePath -DestinationPath $TemporaryDirectory -Force
Copy-Item (Join-Path $TemporaryDirectory "winsuck.exe") $InstallDirectory -Force

$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (($UserPath -split ";") -notcontains $InstallDirectory) {
  [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDirectory", "User")
}
$env:Path = "$env:Path;$InstallDirectory"

Write-Host "Installed winsuck.exe to $InstallDirectory"
Write-Host "Open a new PowerShell or WSL session before relying on the persisted PATH."
```

Kontrollér scriptet i PowerShell før publicering:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\install.ps1 -Version 0.1.0
winsuck.exe --version
```

Dokumentér den nemme installationskommando, når scriptet er versioneret og
gennemgået:

```powershell
irm https://raw.githubusercontent.com/bonest/winsuck/main/scripts/install.ps1 | iex
```

Brugere, der foretrækker at inspicere scriptet først, kan downloade det med
`Invoke-WebRequest`, læse det og derefter køre det lokalt.

## 9. Dokumentér den normale brugeroplevelse

Efter dette setup skal den normale WSL-bruger kun udføre:

```bash
brew tap bonest/tap
brew trust bonest/tap
brew install winsuck
winsuck pull 'C:\AOSService\PackagesLocalDirectory' ~/std-cache/fo-metadata
```

Den manuelle Windows-bruger udfører:

```powershell
irm https://raw.githubusercontent.com/bonest/winsuck/main/scripts/install.ps1 | iex
winsuck.exe send --src C:\AOSService\PackagesLocalDirectory --port 9099
```

## 10. Release-checkliste

Følg denne rækkefølge for hver release:

1. Kør `go test ./...`, `go test -race ./...` og `go vet ./...`.
2. Opdatér changelog og version-afhængig dokumentation.
3. Push en release-branch eller `main` med den ønskede kode.
4. Opret og push tagget, fx `v0.1.0`.
5. Bekræft at GitHub Actions opretter GitHub Release og checksums.
6. Opdatér `Formula/winsuck.rb` i `bonest/homebrew-tap` med release-URL'er og checksums.
7. Kør `brew audit --strict bonest/tap/winsuck`.
8. Test `brew install winsuck` i en ren WSL-installation.
9. Test `winsuck pull` mod et lille Windows-kildetræ.
10. Test PowerShell-installeren mod samme release, hvis den understøttes.

## Fejlsøgning for første release

### `git`-kommandoer fejler eller `git remote -v` er tom

Projektet er ikke forbundet til GitHub endnu. Opret repositoryet i GitHub og
tilføj den remote, GitHub viser. Kør ikke release-tagget før `git push origin
main` virker uden fejl.

### `goreleaser release --snapshot --clean` fejler

Læs den første konkrete fejl i outputtet. De hyppigste årsager er:

- `winsuck --version` mangler, mens `main.version` angives i `ldflags`.
- YAML-indrykning i `.goreleaser.yaml` er forkert.
- GoReleaser-versionen er ældre end konfigurationsformatet i guiden.
- `go test ./...` fejler; ret denne fejl før release-konfigurationen ændres.

Kør først disse kommandoer separat for at afgrænse fejlen:

```bash
go test ./...
go vet ./...
goreleaser check
```

### GitHub Action opretter ingen Release

Workflowet starter kun ved push af et tag, ikke ved et almindeligt commit. Se
alle lokale tags og den valgte remote:

```bash
git tag --list
git ls-remote --tags origin
```

Hvis tagget kun findes lokalt, push det eksplicit:

```bash
git push origin v0.1.0
```

### `brew install winsuck` kan ikke hente formulaen

Kontrollér først at tap-repositoryet er offentligt, og at formulaens filsti er
præcis `Formula/winsuck.rb`. Opdatér lokale tap-data og kør med debug-output:

```bash
brew update
brew tap bonest/tap
brew install --debug bonest/tap/winsuck
```

### Homebrew siger, at checksum ikke matcher

En checksum er bundet til nøjagtig én fil. Hent aldrig en ny release-fil under
samme URL efter at formulaen er publiceret. Opret i stedet en ny version og
opdatér formulaen med checksum fra dens tilsvarende `checksums.txt`.

### `winsuck pull` finder ikke `winsuck.exe`

Find Homebrew-bin-mappen og kontrollér begge filer:

```bash
BIN_DIRECTORY="$(dirname "$(command -v winsuck)")"
ls -l "$BIN_DIRECTORY/winsuck" "$BIN_DIRECTORY/winsuck.exe"
```

Hvis `winsuck.exe` mangler, er formulaens `resource("windows-sender")` ikke
installeret korrekt. Hvis filen findes, men ikke kan afvikles, test den direkte
og kontrollér WSL interoperability som beskrevet i afsnit 7.

## Hvad du ikke behøver at gøre

- Du behøver ikke installere Go på Windows.
- Du behøver ikke installere en Windows-service eller åbne en firewall-port;
  winsuck bruger loopback TCP.
- Du behøver ikke lægge Windows-kodekilden under WSL eller bruge `/mnt/c` til
  selve file transferen.
- Du behøver ikke udgive til den centrale `homebrew/core`; den private tap
  `bonest/tap` er den rigtige første distributionskanal.
