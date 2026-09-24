package transfer

import (
	"archive/tar"
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
	Source  string
	Address string
	Workers int
	Include []string
	Exclude []string
	Update  bool
	DryRun  bool
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

func Send(options SendOptions) (SendResult, error) {
	if options.Workers < 1 {
		options.Workers = runtime.NumCPU()
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
		return collect(options.Source, matcher, options.Workers, Manifest{}, false, nil)
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
	result, err := collect(options.Source, matcher, options.Workers, control.Manifest, options.Update && control.Update, writer)
	if err != nil {
		return result, err
	}
	if err := writer.Close(); err != nil {
		return result, fmt.Errorf("finish tar stream: %w", err)
	}
	return result, nil
}

func collect(root string, matcher *filter.Matcher, workers int, manifest Manifest, update bool, writer *tar.Writer) (SendResult, error) {
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
		if writer == nil {
			continue
		}
		header, err := tar.FileInfoHeader(entry.info, "")
		if err != nil {
			return result, fmt.Errorf("create tar header for %q: %w", entry.rel, err)
		}
		header.Name = entry.rel
		if err := writer.WriteHeader(header); err != nil {
			return result, fmt.Errorf("write tar header for %q: %w", entry.rel, err)
		}
		file, err := os.Open(entry.path)
		if err != nil {
			return result, fmt.Errorf("open %q: %w", entry.path, err)
		}
		written, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			return result, fmt.Errorf("stream %q: %w", entry.rel, copyErr)
		}
		if closeErr != nil {
			return result, fmt.Errorf("close %q: %w", entry.rel, closeErr)
		}
		if written != state.Size {
			return result, fmt.Errorf("source file changed while reading %q", entry.rel)
		}
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
