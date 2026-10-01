package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bonest/winsuck/internal/progress"
)

func TestParseOptionsAcceptsFlagsAfterPaths(t *testing.T) {
	options, positional, err := parseOptions("pull", []string{"C:\\source", "/tmp/destination", "--port", "9123", "--include", "**/*.xpp", "--update"})
	if err != nil {
		t.Fatal(err)
	}
	if len(positional) != 2 || positional[0] != "C:\\source" || positional[1] != "/tmp/destination" {
		t.Fatalf("positional = %#v", positional)
	}
	if options.port != 9123 || !options.update || len(options.include) != 1 {
		t.Fatalf("options = %#v", options)
	}
}

func TestResolveMergesConfigAndCLILists(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "winsuck.json")
	if err := os.WriteFile(configPath, []byte(`{"source":"C:\\source","port":9100,"include":["**/*.xpp"],"exclude":["**/bin/**"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	options, _, err := parseOptions("pull", []string{"--config", configPath, "--port", "9200", "--include", "**/*.xml", "--exclude", "**/*.dll"})
	if err != nil {
		t.Fatal(err)
	}
	project, err := resolve(options)
	if err != nil {
		t.Fatal(err)
	}
	if project.Port != 9200 || project.Source != "C:\\source" {
		t.Fatalf("project = %#v", project)
	}
	if len(project.Include) != 2 || len(project.Exclude) != 2 {
		t.Fatalf("project lists = %#v", project)
	}
}

func TestVersionOutput(t *testing.T) {
	var output bytes.Buffer
	writeVersion(&output)
	if got, want := output.String(), version+"\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestGeneralHelp(t *testing.T) {
	var output bytes.Buffer
	writeGeneralHelp(&output)
	for _, expected := range []string{"pull", "listen", "send", "--version", "--help"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("general help does not contain %q", expected)
		}
	}
}

func TestCommandHelp(t *testing.T) {
	for _, command := range []string{"pull", "listen", "send"} {
		t.Run(command, func(t *testing.T) {
			var output bytes.Buffer
			if err := writeCommandHelp(&output, command); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "Usage:") {
				t.Fatalf("command help = %q", output.String())
			}
		})
	}
}

func TestParseOptionsAcceptsProgress(t *testing.T) {
	options, _, err := parseOptions("pull", []string{"C:\\source", "/tmp/destination", "--progress=json"})
	if err != nil {
		t.Fatal(err)
	}
	if options.progress != progressJSON {
		t.Fatalf("progress = %q, want %q", options.progress, progressJSON)
	}
}

func TestNormalizeProgress(t *testing.T) {
	for _, value := range []string{"", "none", "json", "text"} {
		if _, err := normalizeProgress(value); err != nil {
			t.Errorf("normalizeProgress(%q) returned %v", value, err)
		}
	}
	if _, err := normalizeProgress("xml"); err == nil {
		t.Fatal("normalizeProgress accepted an unknown mode")
	}
}

func TestProgressOutputJSON(t *testing.T) {
	var buffer bytes.Buffer
	output := newProgressOutput(progressJSON, &buffer)
	output.emit(progress.Event{Phase: progress.PhaseDone, FilesDone: 2, BytesDone: 10})

	var event progress.Event
	if err := json.Unmarshal(buffer.Bytes(), &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if event.Phase != progress.PhaseDone || event.FilesDone != 2 || event.BytesDone != 10 {
		t.Fatalf("event = %#v", event)
	}
}

func TestProgressOutputText(t *testing.T) {
	var buffer bytes.Buffer
	output := newProgressOutput(progressText, &buffer)
	output.emit(progress.Event{
		Phase: progress.PhaseTransferring, FilesDone: 1, FilesTotal: 2,
		BytesDone: 1 << 20, BytesTotal: 2 << 20,
	})
	if !strings.Contains(buffer.String(), "1/2 files") || !strings.Contains(buffer.String(), "50%") {
		t.Fatalf("text output = %q", buffer.String())
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:         "0 B",
		1023:      "1023 B",
		1 << 20:   "1.0 MiB",
		5 << 30:   "5.0 GiB",
		1<<62 + 1: "4.0 EiB",
	}
	for value, want := range cases {
		if got := humanBytes(value); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", value, got, want)
		}
	}
}

func TestProgressOutputDisabledByDefault(t *testing.T) {
	var buffer bytes.Buffer
	output := newProgressOutput(progressNone, &buffer)
	if output.Enabled() || output.Sink() != nil {
		t.Fatal("progress output should be disabled for none")
	}
}

func TestForwardSenderProgressKeepsJSONClean(t *testing.T) {
	var buffer bytes.Buffer
	output := newProgressOutput(progressJSON, &buffer)
	diagnostics := forwardSenderProgress(strings.NewReader(
		"{\"phase\":\"transferring\",\"files_done\":2,\"files_total\":4}\n"+
			"{\"phase\":\"error\",\"error\":\"sender exploded\"}\n"+
			"plain diagnostic\n"), output)

	decoder := json.NewDecoder(&buffer)
	var event progress.Event
	if err := decoder.Decode(&event); err != nil {
		t.Fatalf("decode forwarded event: %v", err)
	}
	if event.Phase != progress.PhaseTransferring || event.FilesDone != 2 {
		t.Fatalf("forwarded event = %#v", event)
	}
	if decoder.More() {
		t.Fatal("sender error event or diagnostic leaked into the JSON stream")
	}
	if !strings.Contains(diagnostics, "sender exploded") || !strings.Contains(diagnostics, "plain diagnostic") {
		t.Fatalf("diagnostics = %q", diagnostics)
	}
}

func TestReportErrorJSONIsSingleEvent(t *testing.T) {
	var buffer bytes.Buffer
	output := newProgressOutput(progressJSON, &buffer)
	reportError(output, errors.New("boom"))

	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("json error output = %q, want exactly one line", buffer.String())
	}
	var event progress.Event
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("decode error event: %v", err)
	}
	if event.Version != progress.SchemaVersion || event.Phase != progress.PhaseError || event.Error != "boom" {
		t.Fatalf("error event = %#v", event)
	}
}

func TestRequestedProgressFormat(t *testing.T) {
	if got := requestedProgressFormat([]string{"pull", "C:\\src", "/tmp/dst", "--progress=json"}); got != progressJSON {
		t.Errorf("inline format = %q", got)
	}
	if got := requestedProgressFormat([]string{"send", "--progress", "text"}); got != progressText {
		t.Errorf("separate format = %q", got)
	}
	if got := requestedProgressFormat([]string{"send", "--progress=xml"}); got != progressNone {
		t.Errorf("invalid format = %q", got)
	}
	if got := requestedProgressFormat([]string{"send"}); got != progressNone {
		t.Errorf("missing format = %q", got)
	}
}

func TestHelpAndVersionCommandsDoNotFail(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"--help"}, {"-h"}, {"help"}, {"help", "pull"}, {"pull", "--help"}} {
		if err := run(args); err != nil {
			t.Errorf("run(%q) returned %v", args, err)
		}
	}
}
