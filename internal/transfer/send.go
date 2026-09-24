package transfer

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/bonest/winsuck/internal/filter"
)

type SendOptions struct {
	Source      string
	Address     string
	Workers     int
	ReadWorkers int
	Include     []string
	Exclude     []string
	Update      bool
	DryRun      bool
}

type SendResult struct {
	Files   int64
	Bytes   int64
	Skipped int64
}

type sourceEntry struct {
	path string
	rel  string
	info fs.FileInfo
}

type filePayload struct {
	entry   sourceEntry
	state   FileState
	data    []byte
	large   bool
	skipped bool
	err     error
}

// Files up to maxBufferedFile are read fully into memory by the parallel
// reader pool. Larger files fall back to a serial stream in the writer so
// memory stays bounded. It is a variable so tests can lower the threshold.
var maxBufferedFile int64 = 8 << 20

var errStopped = errors.New("transfer stopped")

func Send(options SendOptions) (SendResult, error) {
	if options.Workers < 1 {
		options.Workers = runtime.NumCPU()
	}
	if options.ReadWorkers < 1 {
		options.ReadWorkers = options.Workers
	}
	ignores, err := filter.ReadIgnore(options.Source)
	if err != nil {
		return SendResult{}, err
	}
	matcher, err := filter.New(options.Include, append(options.Exclude, ignores...))
	if err != nil {
		return SendResult{}, err
	}

	if options.DryRun {
		return collect(options.Source, matcher, options.Workers, 0, Manifest{}, false, nil)
	}

	connection, err := net.Dial("tcp", options.Address)
	if err != nil {
		return SendResult{}, fmt.Errorf("connect to receiver: %w", err)
	}
	defer connection.Close()
	control, err := ReadControl(connection)
	if err != nil {
		return SendResult{}, err
	}
	writer := tar.NewWriter(connection)
	result, err := collect(options.Source, matcher, options.Workers, options.ReadWorkers, control.Manifest, options.Update && control.Update, writer)
	if err != nil {
		return result, err
	}
	if err := writer.Close(); err != nil {
		return result, fmt.Errorf("finish tar stream: %w", err)
	}
	return result, nil
}

func collect(root string, matcher *filter.Matcher, workers, readWorkers int, manifest Manifest, update bool, writer *tar.Writer) (SendResult, error) {
	if writer == nil {
		return scanOnly(root, matcher, workers, manifest, update)
	}
	if readWorkers < 1 {
		readWorkers = 1
	}

	jobs := make(chan string, workers*2)
	entries := make(chan sourceEntry, workers*2)
	payloads := make(chan filePayload, readWorkers*2)
	errs := make(chan error, 1)
	done := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(done) }) }

	report := func(err error) {
		select {
		case errs <- err:
		default:
		}
	}

	var filterWG sync.WaitGroup
	for i := 0; i < workers; i++ {
		filterWG.Add(1)
		go func() {
			defer filterWG.Done()
			for filename := range jobs {
				info, err := os.Lstat(filename)
				if err != nil {
					report(err)
					continue
				}
				rel, err := filepath.Rel(root, filename)
				if err != nil {
					report(err)
					continue
				}
				rel = filepath.ToSlash(rel)
				if !info.Mode().IsRegular() || !matcher.Include(rel) {
					continue
				}
				select {
				case entries <- sourceEntry{path: filename, rel: rel, info: info}:
				case <-done:
					return
				}
			}
		}()
	}

	walkDone := make(chan error, 1)
	go func() {
		defer close(jobs)
		walkDone <- filepath.WalkDir(root, func(filename string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if filename == root {
				return nil
			}
			rel, err := filepath.Rel(root, filename)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if entry.IsDir() {
				if matcher.ExcludesDirectory(rel) {
					return filepath.SkipDir
				}
				return nil
			}
			select {
			case jobs <- filename:
				return nil
			case <-done:
				return errStopped
			}
		})
	}()

	var readWG sync.WaitGroup
	for i := 0; i < readWorkers; i++ {
		readWG.Add(1)
		go func() {
			defer readWG.Done()
			for entry := range entries {
				payload := readEntry(entry, manifest, update)
				select {
				case payloads <- payload:
				case <-done:
					return
				}
			}
		}()
	}

	go func() {
		filterWG.Wait()
		close(entries)
	}()
	go func() {
		readWG.Wait()
		close(payloads)
	}()

	result := SendResult{}
	var firstErr error
	for payload := range payloads {
		if payload.err != nil {
			if firstErr == nil {
				firstErr = payload.err
				stop()
			}
			continue
		}
		if payload.skipped {
			result.Skipped++
			continue
		}
		result.Files++
		result.Bytes += payload.state.Size
		if firstErr != nil {
			continue
		}
		if err := writeEntry(writer, payload); err != nil {
			firstErr = err
			stop()
		}
	}

	if err := <-walkDone; err != nil && !errors.Is(err, errStopped) && firstErr == nil {
		firstErr = fmt.Errorf("walk source: %w", err)
	}
	if firstErr != nil {
		return result, firstErr
	}
	select {
	case err := <-errs:
		return result, fmt.Errorf("inspect source: %w", err)
	default:
	}
	return result, nil
}

func readEntry(entry sourceEntry, manifest Manifest, update bool) filePayload {
	state := fileState(entry.info)
	if update && manifest[entry.rel] == state {
		return filePayload{entry: entry, state: state, skipped: true}
	}
	if entry.info.Size() > maxBufferedFile {
		return filePayload{entry: entry, state: state, large: true}
	}
	file, err := os.Open(entry.path)
	if err != nil {
		return filePayload{entry: entry, state: state, err: fmt.Errorf("open %q: %w", entry.path, err)}
	}
	data, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil {
		return filePayload{entry: entry, state: state, err: fmt.Errorf("read %q: %w", entry.rel, readErr)}
	}
	if closeErr != nil {
		return filePayload{entry: entry, state: state, err: fmt.Errorf("close %q: %w", entry.rel, closeErr)}
	}
	return filePayload{entry: entry, state: state, data: data}
}

func writeEntry(writer *tar.Writer, payload filePayload) error {
	header, err := tar.FileInfoHeader(payload.entry.info, "")
	if err != nil {
		return fmt.Errorf("create tar header for %q: %w", payload.entry.rel, err)
	}
	header.Name = payload.entry.rel
	if err := writer.WriteHeader(header); err != nil {
		return fmt.Errorf("write tar header for %q: %w", payload.entry.rel, err)
	}
	if payload.large {
		return streamFile(writer, payload)
	}
	written, err := writer.Write(payload.data)
	if err != nil {
		return fmt.Errorf("stream %q: %w", payload.entry.rel, err)
	}
	if int64(written) != payload.state.Size {
		return fmt.Errorf("source file changed while reading %q", payload.entry.rel)
	}
	return nil
}

func streamFile(writer *tar.Writer, payload filePayload) error {
	file, err := os.Open(payload.entry.path)
	if err != nil {
		return fmt.Errorf("open %q: %w", payload.entry.path, err)
	}
	written, copyErr := io.Copy(writer, file)
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("stream %q: %w", payload.entry.rel, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %q: %w", payload.entry.rel, closeErr)
	}
	if written != payload.state.Size {
		return fmt.Errorf("source file changed while reading %q", payload.entry.rel)
	}
	return nil
}

func scanOnly(root string, matcher *filter.Matcher, workers int, manifest Manifest, update bool) (SendResult, error) {
	jobs := make(chan string, workers*2)
	entries := make(chan sourceEntry, workers*2)
	errs := make(chan error, 1)
	var workersWG sync.WaitGroup

	for i := 0; i < workers; i++ {
		workersWG.Add(1)
		go func() {
			defer workersWG.Done()
			for filename := range jobs {
				info, err := os.Lstat(filename)
				if err != nil {
					select {
					case errs <- err:
					default:
					}
					continue
				}
				rel, err := filepath.Rel(root, filename)
				if err != nil {
					select {
					case errs <- err:
					default:
					}
					continue
				}
				rel = filepath.ToSlash(rel)
				if !info.Mode().IsRegular() || !matcher.Include(rel) {
					continue
				}
				entries <- sourceEntry{path: filename, rel: rel, info: info}
			}
		}()
	}

	walkDone := make(chan error, 1)
	go func() {
		defer close(jobs)
		walkDone <- filepath.WalkDir(root, func(filename string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if filename == root {
				return nil
			}
			rel, err := filepath.Rel(root, filename)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if entry.IsDir() {
				if matcher.ExcludesDirectory(rel) {
					return filepath.SkipDir
				}
				return nil
			}
			jobs <- filename
			return nil
		})
	}()

	go func() {
		workersWG.Wait()
		close(entries)
	}()

	result := SendResult{}
	for entry := range entries {
		state := fileState(entry.info)
		if update && manifest[entry.rel] == state {
			result.Skipped++
			continue
		}
		result.Files++
		result.Bytes += state.Size
	}
	if err := <-walkDone; err != nil {
		return result, fmt.Errorf("walk source: %w", err)
	}
	select {
	case err := <-errs:
		return result, fmt.Errorf("inspect source: %w", err)
	default:
	}
	return result, nil
}

func fileState(info fs.FileInfo) FileState {
	// archive/tar rounds header timestamps to whole seconds before transmission.
	return FileState{Size: info.Size(), MTime: info.ModTime().Round(time.Second).Unix()}
}

func (r SendResult) String() string {
	return fmt.Sprintf("files=%d bytes=%d skipped=%d", r.Files, r.Bytes, r.Skipped)
}
