package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestHelpAndVersionCommandsDoNotFail(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"--help"}, {"-h"}, {"help"}, {"help", "pull"}, {"pull", "--help"}} {
		if err := run(args); err != nil {
			t.Errorf("run(%q) returned %v", args, err)
		}
	}
}
