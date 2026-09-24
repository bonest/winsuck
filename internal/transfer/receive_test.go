package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeDestination(t *testing.T) {
	root := t.TempDir()
	valid, err := safeDestination(root, "nested/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if valid != filepath.Join(root, "nested", "file.txt") {
		t.Fatalf("valid destination = %q", valid)
	}
	for _, name := range []string{"../escape", "/absolute", ""} {
		if _, err := safeDestination(root, name); err == nil {
			t.Errorf("safeDestination(%q) unexpectedly succeeded", name)
		}
	}
}

func TestSendReceiveAppliesFilters(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "bin", "skip.txt"), []byte("skip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".winsuckignore"), []byte("bin/**\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	listener, err := Listen(ReceiveOptions{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan error, 1)
	go func() {
		_, err := Receive(listener, ReceiveOptions{Destination: destination})
		received <- err
	}()
	result, err := Send(SendOptions{Source: source, Address: listener.Addr().String(), Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-received; err != nil {
		t.Fatal(err)
	}
	if result.Files != 2 { // keep.txt and .winsuckignore
		t.Fatalf("sent %d files, want 2", result.Files)
	}
	data, err := os.ReadFile(filepath.Join(destination, "keep.txt"))
	if err != nil || string(data) != "keep" {
		t.Fatalf("received keep.txt = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(destination, "bin", "skip.txt")); !os.IsNotExist(err) {
		t.Fatalf("excluded file exists or stat failed: %v", err)
	}
}

func TestUpdateSkipsUnchangedFiles(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	for round := 0; round < 2; round++ {
		listener, err := Listen(ReceiveOptions{Address: "127.0.0.1:0"})
		if err != nil {
			t.Fatal(err)
		}
		received := make(chan error, 1)
		go func() {
			_, err := Receive(listener, ReceiveOptions{Destination: destination, Update: true})
			received <- err
		}()
		result, err := Send(SendOptions{Source: source, Address: listener.Addr().String(), Update: true})
		listener.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := <-received; err != nil {
			t.Fatal(err)
		}
		if round == 0 && result.Files != 1 {
			t.Fatalf("initial transfer sent %d files, want 1", result.Files)
		}
		if round == 1 && (result.Files != 0 || result.Skipped != 1) {
			t.Fatalf("update result = %#v, want one skipped file", result)
		}
	}
	if _, err := os.Stat(filepath.Join(destination, manifestName)); err != nil {
		t.Fatalf("manifest was not written: %v", err)
	}
}
