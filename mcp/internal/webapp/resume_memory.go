package webapp

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const maxResumeProcessBytes int64 = 512 * 1024 * 1024

var errResumeMemoryLimit = errors.New("resume extraction exceeded the memory limit")

func resumeResidentBytes(ctx context.Context, pid int) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/ps", "-o", "rss=", "-p", strconv.Itoa(pid))
	command.Env = []string{"LC_ALL=C"}
	command.WaitDelay = time.Second
	output, err := command.Output()
	if err != nil {
		return 0, err
	}
	kib, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if err != nil || kib < 0 || kib > (1<<63-1)/1024 {
		return 0, errors.New("invalid process memory usage")
	}
	return kib * 1024, nil
}

func runResumeCommand(ctx context.Context, command *exec.Cmd, memoryUsage func(context.Context, int) (int64, error)) error {
	if memoryUsage == nil {
		return command.Run()
	}
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	failures := 0
	for {
		select {
		case err := <-done:
			return err
		case <-ticker.C:
			used, err := memoryUsage(ctx, command.Process.Pid)
			select {
			case err := <-done:
				return err
			default:
			}
			if err != nil {
				failures++
			} else {
				failures = 0
			}
			if used > maxResumeProcessBytes || failures >= 3 {
				_ = command.Process.Kill()
				<-done
				if err != nil {
					return errors.New("could not monitor resume extraction memory")
				}
				return errResumeMemoryLimit
			}
		}
	}
}
