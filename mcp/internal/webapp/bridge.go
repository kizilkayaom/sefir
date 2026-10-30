package webapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
)

type Bridge interface {
	Call(context.Context, string, any) (json.RawMessage, error)
}

type PythonBridge struct {
	Python string
	Root   string
	AI     *AIConfiguration
}

func (b PythonBridge) Call(ctx context.Context, action string, payload any) (json.RawMessage, error) {
	input, err := json.Marshal(map[string]any{"action": action, "payload": payload})
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, b.Python, "-m", "sefir_agent.web_bridge")
	command.Dir = b.Root
	if b.AI != nil && (action == "generate" || action == "providers" || action == "models") {
		command.Env, err = b.AI.Environment(os.Environ())
		if err != nil {
			return nil, errors.New("AI configuration is unavailable")
		}
	}
	command.Stdin = bytes.NewReader(input)
	output, err := command.Output()
	if err != nil {
		return nil, errors.New("agent operation failed")
	}
	if len(output) > 1<<20 || !json.Valid(output) {
		return nil, errors.New("agent returned an invalid response")
	}
	return json.RawMessage(output), nil
}
