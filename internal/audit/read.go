// Ver 2026-07-13 19:00, by Sonnet 5

package audit

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// MaxLogLine bounds how much of one JSONL line is held in memory while
// scanning (bodies are recorded in full, so lines can be large). A line
// beyond the cap is skipped — reported through onSkip and counted as a
// parse error by callers — instead of aborting the whole read, which is
// what a plain bufio.Scanner would do on ErrTooLong.
const MaxLogLine = 128 << 20

// OpenLogFile opens an audit JSONL file, transparently decompressing it if
// housekeeping (housekeep.go) has since rotated it into a .zst — historical
// and live files can be mixed in the same glob without the caller caring
// which is which. Shared by every consumer of audit files (report, replay)
// so the compressed-vs-plain decision lives in exactly one place.
func OpenLogFile(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(path, ".zst") {
		return f, nil
	}
	dec, err := zstd.NewReader(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return zstdReadCloser{dec, f}, nil
}

// zstdReadCloser adapts *zstd.Decoder — whose Close takes no error and
// doesn't own the underlying reader — to io.ReadCloser over the file it
// reads from.
type zstdReadCloser struct {
	dec *zstd.Decoder
	f   *os.File
}

func (z zstdReadCloser) Read(p []byte) (int, error) { return z.dec.Read(p) }

func (z zstdReadCloser) Close() error {
	z.dec.Close()
	return z.f.Close()
}

// LineAt returns the raw bytes of path's 1-based logical line — the same
// counting ForEachLine already uses (a too-long skipped line still advances
// the counter, so this stays aligned with whatever wrote the line number a
// caller is passing in, e.g. a ctxgraph coordinate or vmr-requests.json's
// own row order). It does not unmarshal: callers decode into whatever shape
// they need (a full audit.Record for a "read" consumer, a partial view for
// replay). Unlike ForEachLine's callers elsewhere, line 0 is not special
// here — it's simply not found, since a coordinate-addressed line is always
// a concrete positive number.
func LineAt(path string, line int) ([]byte, error) {
	if line <= 0 {
		return nil, fmt.Errorf("%s: line %d is not a valid 1-based line number", path, line)
	}
	rc, err := OpenLogFile(path)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var found []byte
	n := 0
	scanErr := ForEachLine(rc, MaxLogLine, func(lb []byte) {
		n++
		if n == line && found == nil {
			found = append([]byte(nil), lb...) // ForEachLine reuses its buffer; copy before it's overwritten
		}
	}, nil)
	if scanErr != nil {
		return nil, scanErr
	}
	if found == nil {
		return nil, fmt.Errorf("%s: line %d not found", path, line)
	}
	return found, nil
}

// ForEachLine invokes fn for every non-empty line in r (trailing \n
// stripped). Lines longer than maxLine are drained with bounded memory and
// reported via onSkip (nilable) instead of failing the scan. The line slice
// is reused between calls — fn must not retain it.
func ForEachLine(r io.Reader, maxLine int, fn func(line []byte), onSkip func()) error {
	br := bufio.NewReaderSize(r, 1<<20)
	var buf []byte
	tooLong := false
	for {
		frag, err := br.ReadSlice('\n')
		if !tooLong {
			if len(buf)+len(frag) > maxLine {
				tooLong, buf = true, buf[:0]
			} else {
				buf = append(buf, frag...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil && err != io.EOF {
			return err
		}
		line := bytes.TrimSuffix(buf, []byte("\n"))
		switch {
		case tooLong:
			if onSkip != nil {
				onSkip()
			}
		case len(line) > 0:
			fn(line)
		}
		buf, tooLong = buf[:0], false
		if err == io.EOF {
			return nil
		}
	}
}
