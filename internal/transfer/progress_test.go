package transfer

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/bonest/winsuck/internal/progress"
)

type eventLog struct {
	mu     sync.Mutex
	events []progress.Event
}

func (log *eventLog) sink(event progress.Event) {
	log.mu.Lock()
	log.events = append(log.events, event)
	log.mu.Unlock()
}

func (log *eventLog) phases() map[progress.Phase][]progress.Event {
	log.mu.Lock()
	defer log.mu.Unlock()
	phases := map[progress.Phase][]progress.Event{}
	for _, event := range log.events {
		phases[event.Phase] = append(phases[event.Phase], event)
	}
	return phases
}

func TestSendReceiveReportProgress(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("first file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "b.txt"), []byte("second file"), 0o644); err != nil {
		t.Fatal(err)
	}

	listener, err := Listen(ReceiveOptions{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	receiverLog := &eventLog{}
	received := make(chan error, 1)
	go func() {
		_, err := Receive(listener, ReceiveOptions{Destination: destination, Progress: receiverLog.sink})
		received <- err
	}()

	senderLog := &eventLog{}
	result, err := Send(SendOptions{
		Source: source, Address: listener.Addr().String(), Workers: 2, Progress: senderLog.sink,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-received; err != nil {
		t.Fatal(err)
	}

	senderPhases := senderLog.phases()
	if _, ok := senderPhases[progress.PhaseScanning]; !ok {
		t.Fatal("sender did not report a scanning event")
	}
	transferring := senderPhases[progress.PhaseTransferring]
	if len(transferring) == 0 {
		t.Fatal("sender did not report a transferring event")
	}
	if transferring[0].FilesTotal != 2 {
		t.Fatalf("transferring files_total = %d, want 2", transferring[0].FilesTotal)
	}
	done := senderPhases[progress.PhaseDone]
	if len(done) != 1 {
		t.Fatalf("sender done events = %d, want 1", len(done))
	}
	if done[0].FilesDone != result.Files+result.Skipped || done[0].FilesTotal != 2 {
		t.Fatalf("sender done = %#v, result = %#v", done[0], result)
	}
	if done[0].BytesDone != result.Bytes {
		t.Fatalf("sender done bytes = %d, want %d", done[0].BytesDone, result.Bytes)
	}

	receiverDone := receiverLog.phases()[progress.PhaseDone]
	if len(receiverDone) != 1 {
		t.Fatalf("receiver done events = %d, want 1", len(receiverDone))
	}
	if receiverDone[0].FilesDone != result.Files || receiverDone[0].BytesDone != result.Bytes {
		t.Fatalf("receiver done = %#v, result = %#v", receiverDone[0], result)
	}
}
