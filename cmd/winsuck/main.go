package main

import (
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

	"github.com/bonest/winsuck/internal/config"
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
	set         map[string]bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "winsuck:", err)
		writeGeneralHelp(os.Stderr)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		writeGeneralHelp(os.Stdout)
		return nil
	}
	if args[0] == "--version" {
		writeVersion(os.Stdout)
		return nil
	}
	if isHelp(args[0]) {
		return writeRequestedHelp(os.Stdout, args[1:])
	}
	command := args[0]
	if len(args) > 1 && isHelp(args[1]) {
		return writeCommandHelp(os.Stdout, command)
	}
	options, positional, err := parseOptions(command, args[1:])
	if err != nil {
		return err
	}
	project, err := resolve(options)
	if err != nil {
		return err
	}

	switch command {
	case "pull":
		if len(positional) > 2 {
			return errors.New("pull accepts at most <source> <destination>")
		}
		if len(positional) > 0 {
			project.Source = positional[0]
		}
		if len(positional) > 1 {
			project.Destination = positional[1]
		}
		if project.Source == "" || project.Destination == "" {
			return errors.New("pull requires source and destination")
		}
		return pull(project, options.dryRun)
	case "listen":
		if len(positional) > 1 {
			return errors.New("listen accepts at most <destination>")
		}
		if len(positional) == 1 {
			project.Destination = positional[0]
		}
		if project.Destination == "" {
			return errors.New("listen requires destination")
		}
		return listen(project)
	case "send":
		if len(positional) > 1 {
			return errors.New("send accepts at most <source>")
		}
		if len(positional) == 1 {
			project.Source = positional[0]
		}
		if project.Source == "" {
			return errors.New("send requires source")
		}
		result, err := transfer.Send(transfer.SendOptions{
			Source: project.Source, Address: address(project), Workers: project.Workers,
			Include: project.Include, Exclude: project.Exclude, Update: project.Update,
			DryRun: options.dryRun,
		})
		if err == nil {
			fmt.Println(result.String())
		}
		return err
	default:
		return fmt.Errorf("unknown command %q", command)
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
	flags.SetOutput(os.Stderr)
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

func listen(project config.Project) error {
	listener, err := transfer.Listen(transfer.ReceiveOptions{Address: address(project)})
	if err != nil {
		return err
	}
	defer listener.Close()
	result, err := transfer.Receive(listener, transfer.ReceiveOptions{Destination: project.Destination, Update: project.Update})
	if err == nil {
		fmt.Printf("files=%d bytes=%d\n", result.Files, result.Bytes)
	}
	return err
}

func pull(project config.Project, dryRun bool) error {
	if dryRun {
		result, err := transfer.Send(transfer.SendOptions{Source: project.Source, Workers: project.Workers, Include: project.Include, Exclude: project.Exclude, Update: project.Update, DryRun: true})
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
	command.Stderr = os.Stderr
	sendErr := command.Run()
	if sendErr != nil {
		listener.Close()
	}
	receiveErr := <-received
	if sendErr != nil {
		return fmt.Errorf("run Windows sender: %w", sendErr)
	}
	return receiveErr
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
