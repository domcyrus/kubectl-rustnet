package runner

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/domcyrus/kubectl-rustnet/internal/pod"
)

// Options configures the plugin run.
type Options struct {
	Namespace  string
	Kubeconfig string
	Context    string
	Timeout    time.Duration
	Pod        pod.Options
}

// Run creates a debug pod, waits for it to be running, attaches, and cleans up on exit.
func Run(opts Options) error {
	podName := fmt.Sprintf("rustnet-debug-%s", randomSuffix(8))

	overrides, err := pod.BuildOverrides(opts.Pod)
	if err != nil {
		return err
	}

	// Common kubectl flags
	var kubectlFlags []string
	if opts.Kubeconfig != "" {
		kubectlFlags = append(kubectlFlags, "--kubeconfig", opts.Kubeconfig)
	}
	if opts.Context != "" {
		kubectlFlags = append(kubectlFlags, "--context", opts.Context)
	}

	// Cleanup function to delete the pod
	cleanup := func() {
		args := append([]string{
			"delete", "pod", podName,
			"--namespace", opts.Namespace,
			"--grace-period=0", "--force",
			"--ignore-not-found",
		}, kubectlFlags...)
		cmd := exec.Command("kubectl", args...)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}

	// Trap signals for cleanup
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cleanup()
		os.Exit(0)
	}()
	defer cleanup()

	// Step 1: Create the pod (without -it)
	createArgs := append([]string{
		"run", podName,
		"--image", opts.Pod.Image,
		"--restart=Never",
		"--overrides", overrides,
		"--namespace", opts.Namespace,
	}, kubectlFlags...)

	createCmd := exec.Command("kubectl", createArgs...)
	createCmd.Stderr = os.Stderr
	if err := createCmd.Run(); err != nil {
		return fmt.Errorf("failed to create pod: %w", err)
	}

	// Step 2: Wait for the pod to be running
	if err := waitForPod(podName, opts.Namespace, kubectlFlags, 60*time.Second); err != nil {
		return fmt.Errorf("pod failed to start: %w", err)
	}

	// Step 3: Attach to the pod
	ctx := context.Background()
	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	attachArgs := append([]string{
		"attach", podName,
		"--namespace", opts.Namespace,
		"-it",
	}, kubectlFlags...)

	attachCmd := exec.CommandContext(ctx, "kubectl", attachArgs...)
	attachCmd.Stdin = os.Stdin
	attachCmd.Stdout = os.Stdout
	attachCmd.Stderr = os.Stderr

	return attachCmd.Run()
}

// waitForPod polls until the pod is Running or fails/times out.
func waitForPod(name, namespace string, kubectlFlags []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		args := append([]string{
			"get", "pod", name,
			"--namespace", namespace,
			"-o", "jsonpath={.status.phase}",
		}, kubectlFlags...)

		out, err := exec.Command("kubectl", args...).Output()
		if err == nil {
			phase := strings.TrimSpace(string(out))
			switch phase {
			case "Running":
				return nil
			case "Failed", "Succeeded":
				return fmt.Errorf("pod entered %s phase", phase)
			}
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("timed out waiting for pod to start")
}

// BuildArgs returns the kubectl args that would be used (for testing).
func BuildArgs(opts Options) (string, []string, error) {
	podName := fmt.Sprintf("rustnet-debug-%s", "test1234")

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
