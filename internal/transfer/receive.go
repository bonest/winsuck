package transfer

import (
	"archive/tar"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type ReceiveOptions struct {
	Destination string
	Address     string
	Update      bool
}

type ReceiveResult struct {
	Files int64
	Bytes int64
}

func Listen(options ReceiveOptions) (net.Listener, error) {
	return net.Listen("tcp", options.Address)
}

func Receive(listener net.Listener, options ReceiveOptions) (ReceiveResult, error) {
	connection, err := listener.Accept()
	if err != nil {
		return ReceiveResult{}, fmt.Errorf("accept sender: %w", err)
	}
	defer connection.Close()
	if err := os.MkdirAll(options.Destination, 0o755); err != nil {
		return ReceiveResult{}, fmt.Errorf("create destination: %w", err)
	}
	manifest := Manifest{}
	if options.Update {
		manifest, err = ReadManifest(options.Destination)
		if err != nil {
			return ReceiveResult{}, err
		}
	}
	if err := WriteControl(connection, Control{Update: options.Update, Manifest: manifest}); err != nil {
		return ReceiveResult{}, fmt.Errorf("send receiver control: %w", err)
	}

	result := ReceiveResult{}
	if !options.Update {
		manifest = Manifest{}
	}
	reader := tar.NewReader(connection)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, fmt.Errorf("read tar stream: %w", err)
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		filename, err := safeDestination(options.Destination, header.Name)
		if err != nil {
			return result, err
		}
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			return result, fmt.Errorf("create parent for %q: %w", header.Name, err)
		}
		temporary, err := os.CreateTemp(filepath.Dir(filename), ".winsuck-*")
		if err != nil {
			return result, fmt.Errorf("create temporary output for %q: %w", header.Name, err)
		}
		temporaryName := temporary.Name()
		written, copyErr := io.Copy(temporary, reader)
		closeErr := temporary.Close()
		if copyErr != nil || closeErr != nil {
			os.Remove(temporaryName)
			if copyErr != nil {
				return result, fmt.Errorf("extract %q: %w", header.Name, copyErr)
			}
			return result, fmt.Errorf("close %q: %w", header.Name, closeErr)
		}
		if written != header.Size {
			os.Remove(temporaryName)
			return result, fmt.Errorf("truncated tar entry %q", header.Name)
		}
		if err := os.Chmod(temporaryName, os.FileMode(header.Mode)); err != nil {
			os.Remove(temporaryName)
			return result, fmt.Errorf("set permissions on %q: %w", header.Name, err)
		}
		if err := os.Chtimes(temporaryName, header.ModTime, header.ModTime); err != nil {
			os.Remove(temporaryName)
			return result, fmt.Errorf("set timestamp on %q: %w", header.Name, err)
		}
		if err := os.Rename(temporaryName, filename); err != nil {
			os.Remove(temporaryName)
			return result, fmt.Errorf("publish %q: %w", header.Name, err)
		}
		manifest[filepath.ToSlash(header.Name)] = FileState{Size: header.Size, MTime: header.ModTime.Unix()}
		result.Files++
		result.Bytes += written
	}
	if options.Update {
		if err := WriteManifest(options.Destination, manifest); err != nil {
			return result, err
		}
	}
	return result, nil
}

func safeDestination(root, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if name == "" || filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe tar path %q", name)
	}
	return filepath.Join(root, clean), nil
}
