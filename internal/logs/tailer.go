package logs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Tailer streams log entries from a set of files.
type Tailer struct {
	files  []string
	parser ParserConfig

	tailLines int
}

// Options configures the behavior of a Tailer.
type Options struct {
	Parser    ParserConfig
	TailLines int
}

// NewTailer constructs a Tailer for the provided file paths.
func NewTailer(files []string, opts Options) *Tailer {
	return &Tailer{
		files:     append([]string(nil), files...),
		parser:    opts.Parser,
		tailLines: opts.TailLines,
	}
}

// Start begins streaming log entries until the context is canceled.
func (t *Tailer) Start(ctx context.Context) (<-chan LogEntry, <-chan error) {
	entries := make(chan LogEntry, 256)
	errs := make(chan error, 64)

	var wg sync.WaitGroup
	for _, path := range t.files {
		path := path
		wg.Add(1)
		go func() {
			defer wg.Done()
			t.tailFile(ctx, path, entries, errs)
		}()
	}

	go func() {
		wg.Wait()
		close(entries)
		close(errs)
	}()

	return entries, errs
}

type fileState struct {
	offset  int64
	pending string
}

func (t *Tailer) tailFile(ctx context.Context, path string, entries chan<- LogEntry, errs chan<- error) {
	state := &fileState{}
	pollTicker := time.NewTicker(400 * time.Millisecond)
	defer pollTicker.Stop()

	var watcher *fsnotify.Watcher
	dir := filepath.Dir(path)
	if w, err := fsnotify.NewWatcher(); err == nil {
		if err := w.Add(dir); err != nil {
			errs <- fmt.Errorf("watch %s: %w", dir, err)
			_ = w.Close()
		} else {
			watcher = w
			defer watcher.Close()
		}
	} else {
		errs <- fmt.Errorf("fsnotify: %w", err)
	}

	t.emitInitial(ctx, path, state, entries, errs)

	readNewData := func() {
		lines, err := state.readNewLines(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return
			}
			errs <- fmt.Errorf("tail %s: %w", path, err)
			return
		}
		for _, line := range lines {
			if line == "" {
				continue
			}
			entry, err := parseEntry(path, line, t.parser)
			if err != nil {
				errs <- err
				continue
			}
			select {
			case <-ctx.Done():
				return
			case entries <- entry:
			}
		}
	}

	for {
		if watcher == nil {
			select {
			case <-ctx.Done():
				return
			case <-pollTicker.C:
				readNewData()
			case <-time.After(150 * time.Millisecond):
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
			readNewData()
		case event, ok := <-watcher.Events:
			if !ok {
				watcher = nil
				continue
			}
			if eventHasPath(event, path) {
				switch {
				case event.Op&fsnotify.Write == fsnotify.Write:
					readNewData()
				case event.Op&(fsnotify.Remove|fsnotify.Rename|fsnotify.Create) != 0:
					state.reset()
					waitForReappear(ctx, path)
					readNewData()
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				continue
			}
			errs <- fmt.Errorf("watcher error: %w", err)
		}
	}
}

func (t *Tailer) emitInitial(ctx context.Context, path string, state *fileState, entries chan<- LogEntry, errs chan<- error) {
	lines, err := state.readAll(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		errs <- fmt.Errorf("initial read %s: %w", path, err)
		return
	}

	if t.tailLines > 0 && len(lines) > t.tailLines {
		lines = lines[len(lines)-t.tailLines:]
	}

	for _, line := range lines {
		if line == "" {
			continue
		}
		entry, err := parseEntry(path, line, t.parser)
		if err != nil {
			errs <- err
			continue
		}
		select {
		case <-ctx.Done():
			return
		case entries <- entry:
		}
	}
}

func (s *fileState) readAll(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if info, err := file.Stat(); err == nil {
		s.offset = info.Size()
	}
	s.pending = ""
	return lines, nil
}

func (s *fileState) readNewLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	if info.Size() < s.offset {
		s.offset = 0
		s.pending = ""
	}

	if _, err := file.Seek(s.offset, io.SeekStart); err != nil {
		return nil, err
	}

	var lines []string
	reader := bufio.NewReader(file)
	for {
		chunk, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return lines, err
		}
		if len(chunk) > 0 {
			s.offset += int64(len(chunk))
			s.pending += chunk
			for {
				idx := indexOfNewline(s.pending)
				if idx == -1 {
					break
				}
				segment := s.pending[:idx]
				if len(segment) > 0 && segment[len(segment)-1] == '\r' {
					segment = segment[:len(segment)-1]
				}
				lines = append(lines, segment)
				s.pending = s.pending[idx+1:]
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}

	return lines, nil
}

func (s *fileState) reset() {
	s.offset = 0
	s.pending = ""
}

func eventHasPath(event fsnotify.Event, path string) bool {
	if event.Name == path {
		return true
	}
	if filepath.Clean(event.Name) == filepath.Clean(path) {
		return true
	}
	return false
}

func waitForReappear(ctx context.Context, path string) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := os.Stat(path); err == nil {
				return
			}
		}
	}
}

func indexOfNewline(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return i
		}
	}
	return -1
}
