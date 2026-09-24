package transfer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSendReceiveLargeFileFallback(t *testing.T) {
	original := maxBufferedFile
	maxBufferedFile = 8
	t.Cleanup(func() { maxBufferedFile = original })

	source := t.TempDir()
	destination := t.TempDir()
	small := []byte("small")
	large := bytes.Repeat([]byte("large-file-"), 64)
	if err := os.WriteFile(filepath.Join(source, "small.txt"), small, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "large.txt"), large, 0o644); err != nil {
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
	result, err := Send(SendOptions{Source: source, Address: listener.Addr().String(), Workers: 3, ReadWorkers: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-received; err != nil {
		t.Fatal(err)
	}
	if result.Files != 2 {
		t.Fatalf("sent %d files, want 2", result.Files)
	}
	for name, want := range map[string][]byte{"small.txt": small, "large.txt": large} {
		got, err := os.ReadFile(filepath.Join(destination, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s content mismatch: got %d bytes, want %d", name, len(got), len(want))
		}
	}
}
