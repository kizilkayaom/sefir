package webapp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestResumeMemoryMonitorStopsProcess(t *testing.T) {
	for _, test := range []struct {
		name      string
		usage     int64
		readError error
		want      string
	}{
		{name: "over limit", usage: maxResumeProcessBytes + 1, want: errResumeMemoryLimit.Error()},
		{name: "monitor unavailable", readError: errors.New("private system details"), want: "could not monitor resume extraction memory"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, "/bin/sleep", "30")
			calls := 0
			err := runResumeCommand(ctx, command, func(context.Context, int) (int64, error) {
				calls++
				return test.usage, test.readError
			})
			if err == nil || err.Error() != test.want || ctx.Err() != nil {
				t.Fatalf("expected prompt monitored termination, got %v", err)
			}
			if calls == 0 || command.ProcessState == nil || command.ProcessState.Success() {
				t.Fatal("extractor was not monitored, killed, and reaped")
			}
		})
	}
}

func TestResumeMemoryMonitorAllowsCompletionAtLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sleep", "0.5")
	calls := 0
	err := runResumeCommand(ctx, command, func(context.Context, int) (int64, error) {
		calls++
		if calls == 1 {
			return 0, errors.New("temporary measurement failure")
		}
		return maxResumeProcessBytes, nil
	})
	if err != nil || calls < 2 || !command.ProcessState.Success() {
		t.Fatalf("expected successful extraction at the limit, got %v", err)
	}
}

func TestResumeMemoryMonitorHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/sleep", "30")
	err := runResumeCommand(ctx, command, func(context.Context, int) (int64, error) { return 0, nil })
	if err == nil || ctx.Err() != context.DeadlineExceeded || command.ProcessState == nil {
		t.Fatal("cancelled extractor was not stopped and reaped")
	}
}

func TestResumeResidentBytes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("resident memory monitoring is used on macOS")
	}
	used, err := resumeResidentBytes(context.Background(), os.Getpid())
	if err != nil || used <= 0 {
		t.Fatalf("expected positive memory usage, got %d, %v", used, err)
	}
}
