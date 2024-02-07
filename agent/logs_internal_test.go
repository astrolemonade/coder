package agent

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	protobuf "google.golang.org/protobuf/proto"

	"cdr.dev/slog"
	"cdr.dev/slog/sloggers/slogtest"
	"github.com/coder/coder/v2/agent/proto"
	"github.com/coder/coder/v2/coderd/database/dbtime"
	"github.com/coder/coder/v2/codersdk"
	"github.com/coder/coder/v2/codersdk/agentsdk"
	"github.com/coder/coder/v2/testutil"
)

func TestLogSender_Mainline(t *testing.T) {
	t.Parallel()
	testCtx := testutil.Context(t, testutil.WaitShort)
	ctx, cancel := context.WithCancel(testCtx)
	logger := slogtest.Make(t, nil).Leveled(slog.LevelDebug)
	fDest := newFakeLogDest()
	uut := newLogSender(logger)

	t0 := dbtime.Now()

	ls1 := uuid.UUID{0x11}
	err := uut.enqueue(ls1, agentsdk.Log{
		CreatedAt: t0,
		Output:    "test log 0, src 1",
		Level:     codersdk.LogLevelInfo,
	})
	require.NoError(t, err)

	ls2 := uuid.UUID{0x22}
	err = uut.enqueue(ls2,
		agentsdk.Log{
			CreatedAt: t0,
			Output:    "test log 0, src 2",
			Level:     codersdk.LogLevelError,
		},
		agentsdk.Log{
			CreatedAt: t0,
			Output:    "test log 1, src 2",
			Level:     codersdk.LogLevelWarn,
		},
	)
	require.NoError(t, err)

	loopErr := make(chan error, 1)
	go func() {
		err := uut.sendLoop(ctx, fDest)
		loopErr <- err
	}()

	// since neither source has even been flushed, it should immediately flush
	// both, although the order is not controlled
	var logReqs []*proto.BatchCreateLogsRequest
	logReqs = append(logReqs, testutil.RequireRecvCtx(ctx, t, fDest.reqs))
	logReqs = append(logReqs, testutil.RequireRecvCtx(ctx, t, fDest.reqs))
	for _, req := range logReqs {
		require.NotNil(t, req)
		srcID, err := uuid.FromBytes(req.LogSourceId)
		require.NoError(t, err)
		switch srcID {
		case ls1:
			require.Len(t, req.Logs, 1)
			require.Equal(t, "test log 0, src 1", req.Logs[0].GetOutput())
			require.Equal(t, proto.Log_INFO, req.Logs[0].GetLevel())
			require.Equal(t, t0, req.Logs[0].GetCreatedAt().AsTime())
		case ls2:
			require.Len(t, req.Logs, 2)
			require.Equal(t, "test log 0, src 2", req.Logs[0].GetOutput())
			require.Equal(t, proto.Log_ERROR, req.Logs[0].GetLevel())
			require.Equal(t, t0, req.Logs[0].GetCreatedAt().AsTime())
			require.Equal(t, "test log 1, src 2", req.Logs[1].GetOutput())
			require.Equal(t, proto.Log_WARN, req.Logs[1].GetLevel())
			require.Equal(t, t0, req.Logs[1].GetCreatedAt().AsTime())
		default:
			t.Fatal("unknown log source")
		}
	}

	t1 := dbtime.Now()
	err = uut.enqueue(ls1, agentsdk.Log{
		CreatedAt: t1,
		Output:    "test log 1, src 1",
		Level:     codersdk.LogLevelDebug,
	})
	require.NoError(t, err)
	err = uut.flush(ls1)
	require.NoError(t, err)

	req := testutil.RequireRecvCtx(ctx, t, fDest.reqs)
	// give ourselves a 25% buffer if we're right on the cusp of a tick
	require.LessOrEqual(t, time.Since(t1), flushInterval*5/4)
	require.NotNil(t, req)
	require.Len(t, req.Logs, 1)
	require.Equal(t, "test log 1, src 1", req.Logs[0].GetOutput())
	require.Equal(t, proto.Log_DEBUG, req.Logs[0].GetLevel())
	require.Equal(t, t1, req.Logs[0].GetCreatedAt().AsTime())

	cancel()
	err = testutil.RequireRecvCtx(testCtx, t, loopErr)
	require.NoError(t, err)

	// we can still enqueue more logs after sendLoop returns
	err = uut.enqueue(ls1, agentsdk.Log{
		CreatedAt: t1,
		Output:    "test log 2, src 1",
		Level:     codersdk.LogLevelTrace,
	})
	require.NoError(t, err)
}

func TestLogSender_SkipHugeLog(t *testing.T) {
	t.Parallel()
	testCtx := testutil.Context(t, testutil.WaitShort)
	ctx, cancel := context.WithCancel(testCtx)
	logger := slogtest.Make(t, nil).Leveled(slog.LevelDebug)
	fDest := newFakeLogDest()
	uut := newLogSender(logger)

	t0 := dbtime.Now()
	ls1 := uuid.UUID{0x11}
	hugeLog := make([]byte, logOutputMaxBytes+1)
	for i := range hugeLog {
		hugeLog[i] = 'q'
	}
	err := uut.enqueue(ls1,
		agentsdk.Log{
			CreatedAt: t0,
			Output:    string(hugeLog),
			Level:     codersdk.LogLevelInfo,
		},
		agentsdk.Log{
			CreatedAt: t0,
			Output:    "test log 1, src 1",
			Level:     codersdk.LogLevelInfo,
		})
	require.NoError(t, err)

	loopErr := make(chan error, 1)
	go func() {
		err := uut.sendLoop(ctx, fDest)
		loopErr <- err
	}()

	req := testutil.RequireRecvCtx(ctx, t, fDest.reqs)
	require.NotNil(t, req)
	require.Len(t, req.Logs, 1, "it should skip the huge log")
	require.Equal(t, "test log 1, src 1", req.Logs[0].GetOutput())
	require.Equal(t, proto.Log_INFO, req.Logs[0].GetLevel())

	cancel()
	err = testutil.RequireRecvCtx(testCtx, t, loopErr)
	require.NoError(t, err)
}

func TestLogSender_Batch(t *testing.T) {
	t.Parallel()
	testCtx := testutil.Context(t, testutil.WaitShort)
	ctx, cancel := context.WithCancel(testCtx)
	logger := slogtest.Make(t, nil).Leveled(slog.LevelDebug)
	fDest := newFakeLogDest()
	uut := newLogSender(logger)

	t0 := dbtime.Now()
	ls1 := uuid.UUID{0x11}
	var logs []agentsdk.Log
	for i := 0; i < 60000; i++ {
		logs = append(logs, agentsdk.Log{
			CreatedAt: t0,
			Output:    "r",
			Level:     codersdk.LogLevelInfo,
		})
	}
	err := uut.enqueue(ls1, logs...)
	require.NoError(t, err)

	loopErr := make(chan error, 1)
	go func() {
		err := uut.sendLoop(ctx, fDest)
		loopErr <- err
	}()

	// with 60k logs, we should split into two updates to avoid going over 1MiB, since each log
	// is about 21 bytes.
	gotLogs := 0
	req := testutil.RequireRecvCtx(ctx, t, fDest.reqs)
	require.NotNil(t, req)
	gotLogs += len(req.Logs)
	wire, err := protobuf.Marshal(req)
	require.NoError(t, err)
	require.Less(t, len(wire), logOutputMaxBytes, "wire should not exceed 1MiB")
	req = testutil.RequireRecvCtx(ctx, t, fDest.reqs)
	require.NotNil(t, req)
	gotLogs += len(req.Logs)
	wire, err = protobuf.Marshal(req)
	require.NoError(t, err)
	require.Less(t, len(wire), logOutputMaxBytes, "wire should not exceed 1MiB")
	require.Equal(t, 60000, gotLogs)

	cancel()
	err = testutil.RequireRecvCtx(testCtx, t, loopErr)
	require.NoError(t, err)
}

type fakeLogDest struct {
	reqs chan *proto.BatchCreateLogsRequest
}

func (f fakeLogDest) BatchCreateLogs(ctx context.Context, req *proto.BatchCreateLogsRequest) (*proto.BatchCreateLogsResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case f.reqs <- req:
		return &proto.BatchCreateLogsResponse{}, nil
	}
}

func newFakeLogDest() *fakeLogDest {
	return &fakeLogDest{
		reqs: make(chan *proto.BatchCreateLogsRequest),
	}
}
