package runner

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/domcyrus/kubectl-rustnet/internal/pod"
)

// Options configures the plugin run.
type Options struct {
	Namespace    string
	Kubeconfig   string
	Context      string
	Timeout      time.Duration
	OutputDir    string
	OutputFormat string
	Pod          pod.Options
}

// Run uses one shutdown path for normal exit, interruption, and errors.
func Run(opts Options) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx, opts)
}

type session struct {
	opts     Options
	name     string
	localDir string
}

func (s session) command(ctx context.Context, args ...string) *exec.Cmd {
	flags := []string{"--namespace", s.opts.Namespace}
	if s.opts.Kubeconfig != "" {
		flags = append(flags, "--kubeconfig", s.opts.Kubeconfig)
	}
	if s.opts.Context != "" {
		flags = append(flags, "--context", s.opts.Context)
	}
	cmd := exec.CommandContext(ctx, "kubectl", append(flags, args...)...)
	cmd.Stderr = os.Stderr
	return cmd
}

func run(ctx context.Context, opts Options) (result error) {
	if err := configureExports(&opts); err != nil {
		return err
	}
	if opts.Timeout < 0 {
		return fmt.Errorf("timeout must not be negative")
	}
	overrides, err := pod.BuildOverrides(opts.Pod)
	if err != nil {
		return err
	}
	s := session{opts: opts, name: "rustnet-debug-" + randomSuffix(8)}
	if opts.OutputDir != "" {
		if s.localDir, err = prepareDirectory(opts.OutputDir, s.name); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "Debug pod: %s/%s\n", opts.Namespace, s.name)
	create := s.command(ctx, "run", s.name, "--image", opts.Pod.Image, "--restart=Never", "--overrides", overrides)
	if err := create.Run(); err != nil {
		// A failed API response does not prove that creation failed on the server.
		// Never delete possibly running capture when the outcome is uncertain.
		if s.localDir != "" {
			return s.recoveryError(fmt.Errorf("create pod: %w", err))
		}
		return errors.Join(fmt.Errorf("create pod: %w", err), s.finish())
	}
	defer func() { result = errors.Join(result, s.finish()) }()
	startup, cancel := context.WithTimeout(ctx, 60*time.Second)
	terminated, err := s.waitForPod(startup)
	cancel()
	if err != nil {
		return fmt.Errorf("pod failed to start: %w", err)
	}
	if terminated {
		return nil
	}
	attachCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		attachCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	args := []string{"attach", s.name, "-c", "rustnet-debug"}
	headless := false
	for _, arg := range opts.Pod.RustnetArgs {
		if arg == "--headless" {
			headless = true
		}
	}
	if !headless {
		args = append(args, "-it")
	}
	cmd := s.command(attachCtx, args...)
	cmd.Stdin, cmd.Stdout = os.Stdin, os.Stdout
	if err := cmd.Run(); err != nil && attachCtx.Err() == nil {
		return fmt.Errorf("attach: %w", err)
	}
	return nil
}

type podStatus struct {
	Status struct {
		Phase             string `json:"phase"`
		ContainerStatuses []struct {
			Name  string `json:"name"`
			State struct {
				Running    *struct{} `json:"running"`
				Terminated *struct {
					ExitCode int `json:"exitCode"`
				} `json:"terminated"`
			} `json:"state"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

func (s session) waitForPod(ctx context.Context) (bool, error) {
	for {
		data, err := s.command(ctx, "get", "pod", s.name, "-o", "json").Output()
		if err != nil {
			return false, err
		}
		var status podStatus
		if err := json.Unmarshal(data, &status); err != nil {
			return false, err
		}
		helperReady := s.localDir == ""
		captureReady, terminated := false, false
		for _, c := range status.Status.ContainerStatuses {
			if c.Name == pod.ExportContainer {
				helperReady = c.State.Running != nil
			}
			if c.Name == "rustnet-debug" {
				captureReady = c.State.Running != nil || c.State.Terminated != nil
				terminated = c.State.Terminated != nil
				if terminated && s.localDir == "" && c.State.Terminated.ExitCode != 0 {
					return true, fmt.Errorf("RustNet exited with status %d", c.State.Terminated.ExitCode)
				}
			}
		}
		if captureReady && helperReady {
			return terminated, nil
		}
		if status.Status.Phase == "Failed" || status.Status.Phase == "Succeeded" {
			return false, fmt.Errorf("pod entered %s before capture became available", status.Status.Phase)
		}
		if err := pause(ctx); err != nil {
			return false, err
		}
	}
}

func pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(250 * time.Millisecond):
		return nil
	}
}

func (s session) finish() error {
	var captureErr error
	if s.localDir != "" {
		stopCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		code, err := s.stopCapture(stopCtx)
		cancel()
		if err != nil {
			return s.recoveryError(err)
		}
		if code != 0 {
			captureErr = fmt.Errorf("RustNet exited with status %d", code)
		}
		copyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		err = s.copyEvidence(copyCtx)
		cancel()
		if err != nil {
			return errors.Join(captureErr, s.recoveryError(err))
		}
		fmt.Fprintf(os.Stderr, "Evidence saved to %s\n", s.localDir)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"delete", "pod", s.name, "--ignore-not-found", "--wait=true", "--timeout=25s"}
	if s.localDir == "" {
		args = append(args, "--grace-period=0", "--force")
	}
	if err := s.command(ctx, args...).Run(); err != nil {
		return errors.Join(captureErr, fmt.Errorf("delete pod %s/%s: %w", s.opts.Namespace, s.name, err))
	}
	return captureErr
}

// BuildArgs returns the kubectl args that would be used (for testing).
func BuildArgs(opts Options) (string, []string, error) {
	podName := fmt.Sprintf("rustnet-debug-%s", "test1234")

	if err := configureExports(&opts); err != nil {
		return "", nil, err
	}
	overrides, err := pod.BuildOverrides(opts.Pod)
	if err != nil {
		return "", nil, err
	}

	args := []string{
		"run", podName,
		"--image", opts.Pod.Image,
		"--restart=Never",
		"--overrides", overrides,
		"--namespace", opts.Namespace,
	}
	if opts.Kubeconfig != "" {
		args = append(args, "--kubeconfig", opts.Kubeconfig)
	}
	if opts.Context != "" {
		args = append(args, "--context", opts.Context)
	}

	return podName, args, nil
}

func randomSuffix(n int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			b[i] = 'x'
			continue
		}
		b[i] = charset[idx.Int64()]
	}
	return string(b)
}
