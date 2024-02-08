package agentsdk

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/coder/coder/v2/codersdk"
)

type startupLogsWriter struct {
	buf    bytes.Buffer // Buffer to track partial lines.
	ctx    context.Context
	send   func(ctx context.Context, log ...Log) error
	level  codersdk.LogLevel
	source uuid.UUID
}

func (w *startupLogsWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		nl := bytes.IndexByte(p, '\n')
		if nl == -1 {
			break
		}
		cr := 0
		if nl > 0 && p[nl-1] == '\r' {
			cr = 1
		}

		var partial []byte
		if w.buf.Len() > 0 {
			partial = w.buf.Bytes()
			w.buf.Reset()
		}
		err := w.send(w.ctx, Log{
			CreatedAt: time.Now().UTC(), // UTC, like dbtime.Now().
			Level:     w.level,
			Output:    string(partial) + string(p[:nl-cr]),
		})
		if err != nil {
			return n - len(p), err
		}
		p = p[nl+1:]
	}
	if len(p) > 0 {
		_, err := w.buf.Write(p)
		if err != nil {
			return n - len(p), err
		}
	}
	return n, nil
}

func (w *startupLogsWriter) Close() error {
	if w.buf.Len() > 0 {
		defer w.buf.Reset()
		return w.send(w.ctx, Log{
			CreatedAt: time.Now().UTC(), // UTC, like dbtime.Now().
			Level:     w.level,
			Output:    w.buf.String(),
		})
	}
	return nil
}

// LogsWriter returns an io.WriteCloser that sends logs via the
// provided sender. The sender is expected to be non-blocking. Calling
// Close flushes any remaining partially written log lines but is
// otherwise no-op. If the context passed to LogsWriter is
// canceled, any remaining logs will be discarded.
//
// Neither Write nor Close is safe for concurrent use and must be used
// by a single goroutine.
func LogsWriter(ctx context.Context, sender func(ctx context.Context, log ...Log) error, source uuid.UUID, level codersdk.LogLevel) io.WriteCloser {
	return &startupLogsWriter{
		ctx:    ctx,
		send:   sender,
		level:  level,
		source: source,
	}
}
