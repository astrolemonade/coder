package agentsdk_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/codersdk"
	"github.com/coder/coder/v2/codersdk/agentsdk"
)

func TestStartupLogsWriter_Write(t *testing.T) {
	t.Parallel()

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name       string
		ctx        context.Context
		level      codersdk.LogLevel
		source     codersdk.WorkspaceAgentLogSource
		writes     []string
		want       []agentsdk.Log
		wantErr    bool
		closeFirst bool
	}{
		{
			name:   "single line",
			ctx:    context.Background(),
			level:  codersdk.LogLevelInfo,
			writes: []string{"hello world\n"},
			want: []agentsdk.Log{
				{
					Level:  codersdk.LogLevelInfo,
					Output: "hello world",
				},
			},
		},
		{
			name:   "multiple lines",
			ctx:    context.Background(),
			level:  codersdk.LogLevelInfo,
			writes: []string{"hello world\n", "goodbye world\n"},
			want: []agentsdk.Log{
				{
					Level:  codersdk.LogLevelInfo,
					Output: "hello world",
				},
				{
					Level:  codersdk.LogLevelInfo,
					Output: "goodbye world",
				},
			},
		},
		{
			name:   "multiple newlines",
			ctx:    context.Background(),
			level:  codersdk.LogLevelInfo,
			writes: []string{"\n\n", "hello world\n\n\n", "goodbye world\n"},
			want: []agentsdk.Log{
				{
					Level:  codersdk.LogLevelInfo,
					Output: "",
				},
				{
					Level:  codersdk.LogLevelInfo,
					Output: "",
				},
				{
					Level:  codersdk.LogLevelInfo,
					Output: "hello world",
				},
				{
					Level:  codersdk.LogLevelInfo,
					Output: "",
				},
				{
					Level:  codersdk.LogLevelInfo,
					Output: "",
				},
				{
					Level:  codersdk.LogLevelInfo,
					Output: "goodbye world",
				},
			},
		},
		{
			name:   "multiple lines with partial",
			ctx:    context.Background(),
			level:  codersdk.LogLevelInfo,
			writes: []string{"hello world\n", "goodbye world"},
			want: []agentsdk.Log{
				{
					Level:  codersdk.LogLevelInfo,
					Output: "hello world",
				},
			},
		},
		{
			name:       "multiple lines with partial, close flushes",
			ctx:        context.Background(),
			level:      codersdk.LogLevelInfo,
			writes:     []string{"hello world\n", "goodbye world"},
			closeFirst: true,
			want: []agentsdk.Log{
				{
					Level:  codersdk.LogLevelInfo,
					Output: "hello world",
				},
				{
					Level:  codersdk.LogLevelInfo,
					Output: "goodbye world",
				},
			},
		},
		{
			name:   "multiple lines with partial in middle",
			ctx:    context.Background(),
			level:  codersdk.LogLevelInfo,
			writes: []string{"hello world\n", "goodbye", " world\n"},
			want: []agentsdk.Log{
				{
					Level:  codersdk.LogLevelInfo,
					Output: "hello world",
				},
				{
					Level:  codersdk.LogLevelInfo,
					Output: "goodbye world",
				},
			},
		},
		{
			name:   "removes carriage return when grouped with newline",
			ctx:    context.Background(),
			level:  codersdk.LogLevelInfo,
			writes: []string{"hello world\r\n", "\r\r\n", "goodbye world\n"},
			want: []agentsdk.Log{
				{
					Level:  codersdk.LogLevelInfo,
					Output: "hello world",
				},
				{
					Level:  codersdk.LogLevelInfo,
					Output: "\r",
				},
				{
					Level:  codersdk.LogLevelInfo,
					Output: "goodbye world",
				},
			},
		},
		{
			name: "cancel context",
			ctx:  canceledCtx,
			writes: []string{
				"hello world\n",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got []agentsdk.Log
			send := func(ctx context.Context, log ...agentsdk.Log) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				got = append(got, log...)
				return nil
			}
			w := agentsdk.LogsWriter(tt.ctx, send, uuid.New(), tt.level)
			for _, s := range tt.writes {
				_, err := w.Write([]byte(s))
				if err != nil {
					if tt.wantErr {
						return
					}
					t.Errorf("startupLogsWriter.Write() error = %v, wantErr %v", err, tt.wantErr)
				}
			}

			if tt.closeFirst {
				err := w.Close()
				if err != nil {
					t.Errorf("startupLogsWriter.Close() error = %v", err)
					return
				}
			}

			// Compare got and want, but ignore the CreatedAt field.
			for i := range got {
				got[i].CreatedAt = tt.want[i].CreatedAt
			}
			require.Equal(t, tt.want, got)

			err := w.Close()
			if !tt.closeFirst && (err != nil) != tt.wantErr {
				t.Errorf("startupLogsWriter.Close() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}
