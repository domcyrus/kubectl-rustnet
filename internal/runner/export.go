package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/domcyrus/kubectl-rustnet/internal/pod"
)

func configureExports(opts *Options) error {
	if opts.OutputDir == "" {
		if opts.OutputFormat != "" {
			return fmt.Errorf("--output-format requires --output-dir")
		}
		return nil
	}
	format := opts.OutputFormat
	if format == "" {
		format = "both"
	}
	if _, err := pod.ExportFiles(format); err != nil {
		return err
	}
	opts.Pod.ExportFormat = format
	return nil
}

func prepareDirectory(root, name string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}
	dir, err := os.MkdirTemp(root, name+"-")
	if err != nil {
		return "", fmt.Errorf("create session directory: %w", err)
	}
	return dir, nil
}

func (s session) helperArgs(args ...string) []string {
	return append([]string{"exec", s.name, "-c", pod.ExportContainer, "--"}, args...)
}

func (s session) stopCapture(ctx context.Context) (int, error) {
	if err := s.command(ctx, s.helperArgs("touch", pod.ExportPath+"/.stop")...).Run(); err != nil {
		return 0, fmt.Errorf("request capture shutdown: %w", err)
	}
	for {
		data, err := s.command(ctx, s.helperArgs("/bin/sh", "-c", "if [ -f /evidence/.exit-code ]; then cat /evidence/.exit-code; else printf running; fi")...).Output()
		if err != nil {
			return 0, fmt.Errorf("wait for capture flush: %w", err)
		}
		value := strings.TrimSpace(string(data))
		if value != "running" {
			code, err := strconv.Atoi(value)
			if err != nil || code < 0 || code > 255 {
				return 0, fmt.Errorf("invalid capture completion status %q", value)
			}
			return code, nil
		}
		if err := pause(ctx); err != nil {
			return 0, fmt.Errorf("wait for capture flush: %w", err)
		}
	}
}

func (s session) copyEvidence(ctx context.Context) error {
	files, err := pod.ExportFiles(s.opts.Pod.ExportFormat)
	if err != nil {
		return err
	}
	for _, name := range files {
		if err := s.copyFile(ctx, name); err != nil {
			return fmt.Errorf("copy %s: %w", name, err)
		}
	}
	return nil
}

func (s session) copyFile(ctx context.Context, name string) (result error) {
	remote := pod.ExportPath + "/" + name
	data, err := s.command(ctx, s.helperArgs("sha256sum", remote)...).Output()
	if err != nil {
		return fmt.Errorf("read remote checksum: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || fields[1] != remote {
		return fmt.Errorf("invalid remote checksum output")
	}
	expected, err := hex.DecodeString(fields[0])
	if err != nil || len(expected) != sha256.Size {
		return fmt.Errorf("invalid remote checksum")
	}
	partial := filepath.Join(s.localDir, name+".partial")
	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()
	digest := sha256.New()
	cmd := s.command(ctx, s.helperArgs("cat", remote)...)
	cmd.Stdout = io.MultiWriter(file, digest)
	if err := cmd.Run(); err != nil {
		return err
	}
	if hex.EncodeToString(digest.Sum(nil)) != fields[0] {
		return fmt.Errorf("checksum mismatch; partial download retained")
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	file = nil
	return os.Rename(partial, filepath.Join(s.localDir, name))
}

func (s session) recoveryError(err error) error {
	args := []string{"kubectl", "--namespace", s.opts.Namespace}
	if s.opts.Kubeconfig != "" {
		args = append(args, "--kubeconfig", s.opts.Kubeconfig)
	}
	if s.opts.Context != "" {
		args = append(args, "--context", s.opts.Context)
	}
	args = append(args, "cp", "-c", pod.ExportContainer, s.name+":"+pod.ExportPath+"/.", s.localDir)
	for i, arg := range args {
		args[i] = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
	}
	return fmt.Errorf("%w; pod %s/%s retained. After capture stops, recover files with:\n%s\nThen delete the pod manually", err, s.opts.Namespace, s.name, strings.Join(args, " "))
}
