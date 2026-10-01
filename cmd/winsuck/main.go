package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/bonest/winsuck/internal/config"
	"github.com/bonest/winsuck/internal/progress"
	"github.com/bonest/winsuck/internal/transfer"
)

var version = "dev"

type cliOptions struct {
	configPath  string
	source      string
	destination string
	host        string
	port        int
	workers     int
	include     []string
	exclude     []string
	update      bool
	dryRun      bool
	progress    string
	set         map[string]bool
}

func main() {
	output, err := runWithOutput(os.Args[1:])
	if err == nil {
		return
	}
	reportError(output, err)
	os.Exit(1)
}

// reportError renders a terminal failure. In JSON mode the error is a single
// event so that every stderr line remains machine-readable; otherwise it uses
// the human-readable message and help.
func reportError(output *progressOutput, err error) {
	if output != nil && output.Enabled() {
		output.emitError(err)
		return
	}
	fmt.Fprintln(os.Stderr, "winsuck:", err)
	writeGeneralHelp(os.Stderr)
}

func run(args []string) error {
	_, err := runWithOutput(args)
	return err
}

func runWithOutput(args []string) (*progressOutput, error) {
	if len(args) == 0 {
		writeGeneralHelp(os.Stdout)
		return nil, nil
	}
	if args[0] == "--version" {
		writeVersion(os.Stdout)
		return nil, nil
	}
	if isHelp(args[0]) {
		return nil, writeRequestedHelp(os.Stdout, args[1:])
	}
	command := args[0]
	if len(args) > 1 && isHelp(args[1]) {
		return nil, writeCommandHelp(os.Stdout, command)
	}
	options, positional, err := parseOptions(command, args[1:])
	if err != nil {
		return newProgressOutput(requestedProgressFormat(args), os.Stderr), err
	}
	format, err := normalizeProgress(options.progress)
	if err != nil {
		return newProgressOutput(progressNone, os.Stderr), err
	}
	output := newProgressOutput(format, os.Stderr)
	project, err := resolve(options)
	if err != nil {
		return output, err
	}

	switch command {
	case "pull":
		if len(positional) > 2 {
			return output, errors.New("pull accepts at most <source> <destination>")
		}
		if len(positional) > 0 {
			project.Source = positional[0]
		}
		if len(positional) > 1 {
			project.Destination = positional[1]
		}
		if project.Source == "" || project.Destination == "" {
			return output, errors.New("pull requires source and destination")
		}
		return output, pull(project, options.dryRun, output)
	case "listen":
		if len(positional) > 1 {
			return output, errors.New("listen accepts at most <destination>")
		}
		if len(positional) == 1 {
			project.Destination = positional[0]
		}
		if project.Destination == "" {
			return output, errors.New("listen requires destination")
		}
		return output, listen(project, output.Sink())
	case "send":
		if len(positional) > 1 {
			return output, errors.New("send accepts at most <source>")
		}
		if len(positional) == 1 {
			project.Source = positional[0]
		}
		if project.Source == "" {
			return output, errors.New("send requires source")
		}
		result, err := transfer.Send(transfer.SendOptions{
			Source: project.Source, Address: address(project), Workers: project.Workers,
			Include: project.Include, Exclude: project.Exclude, Update: project.Update,
			DryRun: options.dryRun, Progress: output.Sink(),
		})
		if err == nil {
			fmt.Println(result.String())
		}
		return output, err
	default:
		return output, fmt.Errorf("unknown command %q", command)
	}
}

func isHelp(argument string) bool {
	return argument == "--help" || argument == "-h" || argument == "help"
}

func writeRequestedHelp(output io.Writer, args []string) error {
	if len(args) == 0 {
		writeGeneralHelp(output)
		return nil
	}
	if len(args) == 1 {
		return writeCommandHelp(output, args[0])
	}
	return errors.New("help accepts at most one command")
}

func writeVersion(output io.Writer) {
	fmt.Fprintln(output, version)
}

func writeGeneralHelp(output io.Writer) {
	fmt.Fprint(output, `Usage:
  winsuck pull <source> <destination> [flags]
  winsuck listen [<destination>] [flags]
  winsuck send [<source>] [flags]

Commands:
  pull    Run the WSL receiver and start winsuck.exe on Windows.
  listen  Receive a manual Windows sender and extract to WSL ext4.
  send    Traverse a Windows source tree and stream it to a receiver.

Global options:
  --help, -h  Show this help or command-specific help.
  --version   Print the winsuck version.

Run "winsuck help <command>" or "winsuck <command> --help" for command options.
`)
}

func writeCommandHelp(output io.Writer, command string) error {
	switch command {
	case "pull":
		fmt.Fprint(output, `Usage:
  winsuck pull <source> <destination> [flags]
  winsuck pull --config <profile.json> [flags]

Runs the WSL receiver, then starts winsuck.exe through WSL interoperability.

Options:
  --config <file>       JSON project profile.
  --port <port>         Loopback TCP port (default: 9099).
  --workers <count>     Parallel Windows metadata workers (default: CPU cores).
  --include <glob>      Include matching files; repeatable.
  --exclude <glob>      Exclude matching files; repeatable and takes precedence.
  --update              Skip files with unchanged relative path, size, and mtime.
  --dry-run             Scan and report files without transferring.
  --progress <mode>     Progress output on stderr: none, json, or text (default: none).

CLI scalar options override the profile. CLI include/exclude patterns append to it.
`)
	case "listen":
		fmt.Fprint(output, `Usage:
  winsuck listen --dest <destination> [flags]
  winsuck listen <destination> [flags]

Wait for a manual winsuck.exe sender and extract its TAR stream into WSL ext4.

Options:
  --dest <path>         WSL destination directory (required unless positional).
  --config <file>       JSON project profile.
  --host <host>         Listen host (default: 127.0.0.1).
  --port <port>         Loopback TCP port (default: 9099).
  --update              Maintain .winsuck-manifest.json after a successful transfer.
  --progress <mode>     Progress output on stderr: none, json, or text (default: none).
`)
	case "send":
		fmt.Fprint(output, `Usage:
  winsuck send --src <source> [flags]
  winsuck send <source> [flags]


Options:
  --src <path>          Windows source directory (required unless positional).
  --config <file>       JSON project profile.
  --host <host>         Receiver host (default: 127.0.0.1).
  --port <port>         Loopback TCP port (default: 9099).
  --workers <count>     Parallel metadata workers (default: CPU cores).
  --include <glob>      Include matching files; repeatable.
  --exclude <glob>      Exclude matching files; repeatable and takes precedence.
  --update              Use receiver-provided manifest to skip unchanged files.
  --dry-run             Scan and report files without connecting.
  --progress <mode>     Progress output on stderr: none, json, or text (default: none).

.winsuckignore in the source root adds exclude patterns.
`)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	return nil
}

func parseOptions(command string, args []string) (cliOptions, []string, error) {
	options := cliOptions{set: map[string]bool{}}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	// Errors are rendered by reportError; suppressing the flag package's own
	// usage output keeps JSON progress mode free of non-event stderr lines.
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "JSON project configuration")
	flags.Func("src", "source path", setString(&options.source, options.set, "source"))
	flags.Func("dest", "destination path", setString(&options.destination, options.set, "destination"))
	flags.Func("host", "receiver host", setString(&options.host, options.set, "host"))
	flags.Func("port", "TCP port", setInt(&options.port, options.set, "port", 1, 65535))
	flags.Func("workers", "parallel metadata workers", setInt(&options.workers, options.set, "workers", 1, 1<<31-1))
	flags.Func("include", "include glob (repeatable)", func(value string) error {
		options.include = append(options.include, value)
		return nil
	})
	flags.Func("exclude", "exclude glob (repeatable)", func(value string) error {
		options.exclude = append(options.exclude, value)
		return nil
	})
	flags.StringVar(&options.progress, "progress", "", "progress output: none, json, or text")
	flags.Var(&boolFlag{target: &options.update, set: options.set, name: "update"}, "update", "incremental synchronization")
	flags.Var(&boolFlag{target: &options.dryRun, set: options.set, name: "dry-run"}, "dry-run", "scan without transfer")
	normalized, err := reorderFlags(args)
	if err != nil {
		return options, nil, err
	}
	if err := flags.Parse(normalized); err != nil {
		return options, nil, err
	}
	return options, flags.Args(), nil
}

// The documented CLI permits flags after positional source/destination paths,
// while flag.FlagSet normally stops parsing at the first positional argument.
func reorderFlags(args []string) ([]string, error) {
	var flags, positional []string
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if !strings.HasPrefix(argument, "--") || argument == "--" {
			positional = append(positional, argument)
			continue
		}
		flags = append(flags, argument)
		if strings.Contains(argument, "=") || argument == "--update" || argument == "--dry-run" {
			continue
		}
		if index+1 == len(args) {
			return nil, fmt.Errorf("flag %q requires a value", argument)
		}
		index++
		flags = append(flags, args[index])
	}
	return append(flags, positional...), nil
}

func setString(target *string, set map[string]bool, name string) func(string) error {
	return func(value string) error { *target = value; set[name] = true; return nil }
}

func setInt(target *int, set map[string]bool, name string, min, max int) func(string) error {
	return func(value string) error {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < min || parsed > max {
			return fmt.Errorf("invalid %s %q", name, value)
		}
		*target = parsed
		set[name] = true
		return nil
	}
}

type boolFlag struct {
	target *bool
	set    map[string]bool
	name   string
}

func (flag *boolFlag) String() string {
	return strconv.FormatBool(*flag.target)
}

func (flag *boolFlag) Set(value string) error {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return err
	}
	*flag.target = parsed
	flag.set[flag.name] = true
	return nil
}

func (flag *boolFlag) IsBoolFlag() bool {
	return true
}

const (
	progressNone = "none"
	progressJSON = "json"
	progressText = "text"
)

func normalizeProgress(value string) (string, error) {
	switch value {
	case "", progressNone:
		return progressNone, nil
	case progressJSON, progressText:
		return value, nil
	default:
		return "", fmt.Errorf("invalid --progress %q (want %s, %s, or %s)", value, progressNone, progressJSON, progressText)
	}
}

// requestedProgressFormat scans raw arguments before flag parsing so that
// command-line errors can still be reported in the requested format. It returns
// only json or text; anything else (including an invalid value) yields "none"
// and therefore the human-readable error path.
func requestedProgressFormat(args []string) string {
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--progress" && index+1 < len(args) {
			format, err := normalizeProgress(args[index+1])
			if err == nil {
				if format == progressJSON || format == progressText {
					return format
				}
				return progressNone
			}
			return progressNone
		}
		if strings.HasPrefix(argument, "--progress=") {
			format, err := normalizeProgress(strings.TrimPrefix(argument, "--progress="))
			if err == nil {
				if format == progressJSON || format == progressText {
					return format
				}
				return progressNone
			}
			return progressNone
		}
	}
	return progressNone
}

// progressOutput serializes progress events for the calling program. JSON mode
// writes newline-delimited JSON; text mode renders a single carriage-return
// status line. A mutex keeps output well-formed because transfers may report
// from multiple goroutines.
type progressOutput struct {
	mu      sync.Mutex
	format  string
	output  io.Writer
	encoder *json.Encoder
	lastLen int
}

func newProgressOutput(format string, output io.Writer) *progressOutput {
	writer := &progressOutput{format: format, output: output}
	if format == progressJSON {
		writer.encoder = json.NewEncoder(output)
	}
	return writer
}

func (writer *progressOutput) Enabled() bool {
	return writer != nil && writer.format != "" && writer.format != progressNone
}

func (writer *progressOutput) Sink() progress.Sink {
	if !writer.Enabled() {
		return nil
	}
	return writer.emit
}

func (writer *progressOutput) emitError(err error) {
	writer.emit(progress.Event{Phase: progress.PhaseError, Error: err.Error()})
}

func (writer *progressOutput) emit(event progress.Event) {
	event.Version = progress.SchemaVersion
	writer.mu.Lock()
	defer writer.mu.Unlock()
	switch writer.format {
	case progressJSON:
		_ = writer.encoder.Encode(event)
	case progressText:
		line := formatProgress(event)
		padding := ""
		if len(line) < writer.lastLen {
			padding = strings.Repeat(" ", writer.lastLen-len(line))
		}
		fmt.Fprintf(writer.output, "\r%s%s", line, padding)
		writer.lastLen = len(line)
		if event.Phase == progress.PhaseDone || event.Phase == progress.PhaseError {
			fmt.Fprintln(writer.output)
			writer.lastLen = 0
		}
	}
}

func formatProgress(event progress.Event) string {
	switch event.Phase {
	case progress.PhaseError:
		return "error: " + event.Error
	case progress.PhaseDone:
		return fmt.Sprintf("done: files=%d bytes=%d skipped=%d", event.FilesDone, event.BytesDone, event.Skipped)
	}
	prefix := string(event.Phase)
	if event.FilesTotal > 0 {
		percent := float64(event.FilesDone) / float64(event.FilesTotal) * 100
		return fmt.Sprintf("%s: %d/%d files  %s/%s  %.0f%%", prefix, event.FilesDone, event.FilesTotal, humanBytes(event.BytesDone), humanBytes(event.BytesTotal), percent)
	}
	return fmt.Sprintf("%s: %d files  %s", prefix, event.FilesDone, humanBytes(event.BytesDone))
}

func humanBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	divisor, exponent := int64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		divisor *= unit
		exponent++
	}
	return fmt.Sprintf("%.1f %ciB", float64(value)/float64(divisor), "KMGTPE"[exponent])
}

func resolve(options cliOptions) (config.Project, error) {
	project := config.Project{}
	if options.configPath != "" {
		loaded, err := config.Load(options.configPath)
		if err != nil {
			return project, err
		}
		project = loaded
	}
	if options.set["source"] {
		project.Source = options.source
	}
	if options.set["destination"] {
		project.Destination = options.destination
	}
	if options.set["host"] {
		project.Host = options.host
	}
	if options.set["port"] {
		project.Port = options.port
	}
	if options.set["workers"] {
		project.Workers = options.workers
	}
	if options.set["update"] {
		project.Update = options.update
	}
	project.Include = append(project.Include, options.include...)
	project.Exclude = append(project.Exclude, options.exclude...)
	if project.Host == "" {
		project.Host = "127.0.0.1"
	}
	if project.Port == 0 {
		project.Port = config.DefaultPort
	}
	if project.Workers == 0 {
		project.Workers = runtime.NumCPU()
	}
	return project, nil
}

func address(project config.Project) string {
	return net.JoinHostPort(project.Host, strconv.Itoa(project.Port))
}

func listen(project config.Project, sink progress.Sink) error {
	listener, err := transfer.Listen(transfer.ReceiveOptions{Address: address(project)})
	if err != nil {
		return err
	}
	defer listener.Close()
	result, err := transfer.Receive(listener, transfer.ReceiveOptions{Destination: project.Destination, Update: project.Update, Progress: sink})
	if err == nil {
		fmt.Printf("files=%d bytes=%d\n", result.Files, result.Bytes)
	}
	return err
}

func pull(project config.Project, dryRun bool, output *progressOutput) error {
	if dryRun {
		result, err := transfer.Send(transfer.SendOptions{Source: project.Source, Workers: project.Workers, Include: project.Include, Exclude: project.Exclude, Update: project.Update, DryRun: true, Progress: output.Sink()})
		if err == nil {
			fmt.Println(result.String())
		}
		return err
	}
	listener, err := transfer.Listen(transfer.ReceiveOptions{Address: address(project)})
	if err != nil {
		return err
	}
	defer listener.Close()
	received := make(chan error, 1)
	go func() {
		result, receiveErr := transfer.Receive(listener, transfer.ReceiveOptions{Destination: project.Destination, Update: project.Update})
		if receiveErr == nil {
			fmt.Printf("files=%d bytes=%d\n", result.Files, result.Bytes)
		}
		received <- receiveErr
	}()

	sender, err := findWindowsSender()
	if err != nil {
		listener.Close()
		<-received
		return err
	}
	arguments := []string{"send", "--src", project.Source, "--host", project.Host, "--port", strconv.Itoa(project.Port), "--workers", strconv.Itoa(project.Workers)}
	if project.Update {
		arguments = append(arguments, "--update=true")
	}
	for _, pattern := range project.Include {
		arguments = append(arguments, "--include", pattern)
	}
	for _, pattern := range project.Exclude {
		arguments = append(arguments, "--exclude", pattern)
	}
	command := exec.Command(sender, arguments...)
	command.Stdout = os.Stdout
	sendErr := runSender(command, output)
	if sendErr != nil {
		listener.Close()
	}
	receiveErr := <-received
	if sendErr != nil {
		return fmt.Errorf("run Windows sender: %w", sendErr)
	}
	return receiveErr
}

// maxSenderDiagnostics bounds how much non-event sender output is folded into
// a failure message so that stderr stays pure JSON without accumulating
// unbounded diagnostics.
const maxSenderDiagnostics = 10

// runSender starts the Windows sender and forwards its structured progress.
// When progress is enabled the sender writes newline-delimited JSON to stderr.
// Non-event lines and the sender's own error event are kept out of the JSON
// stream; the most recent diagnostics are attached to a returned error so a
// failure still carries context.
func runSender(command *exec.Cmd, output *progressOutput) error {
	if !output.Enabled() {
		command.Stderr = os.Stderr
		return command.Run()
	}
	command.Args = append(command.Args, "--progress=json")
	stderr, err := command.StderrPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	diagnostics := forwardSenderProgress(stderr, output)
	if err := command.Wait(); err != nil {
		if diagnostics != "" {
			return fmt.Errorf("%w: %s", err, diagnostics)
		}
		return err
	}
	return nil
}

// forwardSenderProgress forwards sender events and returns buffered
// diagnostics. Events are re-emitted in the caller's format; the sender's
// terminal error event is dropped because the top-level command owns the single
// error event. Lines that are not events are not written to the output streams.
func forwardSenderProgress(reader io.Reader, output *progressOutput) string {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var diagnostics []string
	appendDiagnostic := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		diagnostics = append(diagnostics, line)
		if len(diagnostics) > maxSenderDiagnostics {
			diagnostics = diagnostics[len(diagnostics)-maxSenderDiagnostics:]
		}
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		var event progress.Event
		if len(line) > 0 && line[0] == '{' && json.Unmarshal(line, &event) == nil && event.Phase != "" {
			if event.Phase == progress.PhaseError {
				appendDiagnostic(event.Error)
				continue
			}
			output.emit(event)
			continue
		}
		appendDiagnostic(string(line))
	}
	return strings.Join(diagnostics, "; ")
}

func findWindowsSender() (string, error) {
	current, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(current), "winsuck.exe")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	path, err := exec.LookPath("winsuck.exe")
	if err != nil {
		return "", errors.New("could not find winsuck.exe next to winsuck or on Windows PATH")
	}
	return path, nil
}
