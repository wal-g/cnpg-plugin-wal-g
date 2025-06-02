/*
Copyright 2025 YANDEX LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package cmd same as exec module but with zombie processes reaper support.
package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/go-logr/logr"
)

type CmdRunResult struct {
	stdout []byte
	stderr []byte
	state  *ProcessState
}

func (c *CmdRunResult) Stdout() []byte {
	return c.stdout
}

func (c *CmdRunResult) Stderr() []byte {
	return c.stderr
}

func (c *CmdRunResult) State() *ProcessState {
	return c.state
}

type CmdBuilder struct {
	ctx    context.Context
	name   string
	envMap map[string]string
	args   []string
}

// Returns new command builder, which can be runned via Run() method
func New(name string, arg ...string) CmdBuilder {
	return CmdBuilder{
		ctx:    context.Background(),
		name:   name,
		envMap: make(map[string]string, 0),
		args:   arg,
	}
}

// WithEnv returns new CmdBuilder with added environment variables for command
// Added variables override existing env values with same name
func (c CmdBuilder) WithEnv(envMap map[string]string) CmdBuilder {
	for key, value := range envMap {
		c.envMap[key] = value
	}
	return c
}

// WithContext returns new CmdBuilder with context, which will be applied to command execution
func (c CmdBuilder) WithContext(ctx context.Context) CmdBuilder {
	c.ctx = ctx
	return c
}

// envsList returns list of user-specified env variables according to exec.Cmd.Env structure
func (c CmdBuilder) envsList() []string {
	envsList := make([]string, 0, len(c.envMap))
	for key, value := range c.envMap {
		envsList = append(envsList, fmt.Sprintf("%s=%s", key, value))
	}
	return envsList
}

// Run executes a command with context, awaits completion and returnes result
func (c CmdBuilder) Run() (result *CmdRunResult, err error) {
	logger := logr.FromContextOrDiscard(c.ctx).WithValues("entrypoint", c.name, "args", c.args)

	result = &CmdRunResult{
		stdout: make([]byte, 0),
		stderr: make([]byte, 0),
		state:  nil,
	}

	cmd := exec.CommandContext(c.ctx, c.name, c.args...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, c.envsList()...)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	cmdExitSubscription := make(chan ProcessInfo, 8)
	subscribeOnProcessExits(cmdExitSubscription)
	defer unsubscribeFromProcessExits(cmdExitSubscription)

	if err = cmd.Start(); err != nil {
		return result, fmt.Errorf("subprocess cmd.Start() error: %w", err)
	}
	logger = logger.WithValues("pid", cmd.Process.Pid, "env", cmd.Env)
	logger.V(1).Info("Starting subprocess")

	cmdWaitStatus, err := wait(logr.NewContext(c.ctx, logger), cmd.Process.Pid, cmdExitSubscription)
	cmd.Wait() // runnning explicit cmd.Wait to finish stdout/stderr piping && do resources cleanup

	result.stdout = stdoutBuf.Bytes()
	result.stderr = stderrBuf.Bytes()
	result.state = &ProcessState{
		pid:    cmd.Process.Pid,
		status: cmdWaitStatus,
	}

	logger.V(1).Info("Finished subprocess", "result", result.State().String())

	if err != nil {
		return result, fmt.Errorf("subprocess run error while waiting cmd %s: %w", cmd.ProcessState.String(), err)
	}

	if result.State().ExitCode() != 0 {
		return result, fmt.Errorf("subprocess run cmd result %s", result.State().String())
	}

	return result, nil
}

func wait(ctx context.Context, pid int, ch chan ProcessInfo) (syscall.WaitStatus, error) {
	logger := logr.FromContextOrDiscard(ctx)
	for {
		select {
		case processInfo := <-ch:
			logger.V(1).Info(fmt.Sprintf("Received notification on process with pid %d finished", processInfo.Pid))
			if processInfo.Pid == pid {
				return processInfo.Status, nil
			}
		case <-ctx.Done():
			return 0, fmt.Errorf("context deadline exceeded")
		}
	}
}
