package loguicapi

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// DockerClient interface for testability.
type DockerClient interface {
	ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error)
	ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error)
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
}

// ContainerAggregator captures stdout/stderr from Docker containers
// and re-emits them as structured log records through the slog pipeline.
type ContainerAggregator struct {
	client     DockerClient
	containers []string // container names to capture
	wg         sync.WaitGroup
	cancel     context.CancelFunc
}

// NewContainerAggregator creates a new aggregator with the given Docker client.
func NewContainerAggregator(dockerClient DockerClient, containers []string) *ContainerAggregator {
	return &ContainerAggregator{
		client:     dockerClient,
		containers: containers,
	}
}

// Start begins log capture for all configured containers.
// Spawns a goroutine per container. Returns immediately.
func (ca *ContainerAggregator) Start(ctx context.Context) {
	ctx, ca.cancel = context.WithCancel(ctx)

	for _, name := range ca.containers {
		ca.wg.Add(1)
		go ca.captureContainer(ctx, name)
	}
}

// Stop cancels all capture goroutines and waits for them to exit (up to 5s).
func (ca *ContainerAggregator) Stop() {
	if ca.cancel != nil {
		ca.cancel()
	}

	done := make(chan struct{})
	go func() {
		ca.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		slog.Warn("container aggregator stop timeout", "msg", "some goroutines did not exit within 5s")
	}
}

// captureContainer captures logs for a single container.
func (ca *ContainerAggregator) captureContainer(ctx context.Context, containerName string) {
	defer ca.wg.Done()

	// Resolve container name to ID
	containerID, err := ca.resolveContainer(ctx, containerName)
	if err != nil {
		slog.Warn("container not found, skipping log capture",
			"container", containerName,
			"error", err,
		)
		return
	}

	// Stream container logs
	opts := container.LogsOptions{
		Follow:     true,
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "0",
	}

	stream, err := ca.client.ContainerLogs(ctx, containerID, opts)
	if err != nil {
		slog.Warn("container log stream failed",
			"container", containerName,
			"error", err,
		)
		return
	}
	defer stream.Close()

	// Demux stdout/stderr
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	// Demux goroutine
	var demuxWg sync.WaitGroup
	demuxWg.Add(2)

	go func() {
		defer demuxWg.Done()
		defer stdoutWriter.Close()
		ca.processLines(ctx, stdoutReader, containerName, "stdout")
	}()

	go func() {
		defer demuxWg.Done()
		defer stderrWriter.Close()
		ca.processLines(ctx, stderrReader, containerName, "stderr")
	}()

	// StdCopy demuxes Docker's multiplexed stream
	_, demuxErr := stdcopy.StdCopy(stdoutWriter, stderrWriter, stream)
	stdoutWriter.Close()
	stderrWriter.Close()
	demuxWg.Wait()

	if demuxErr != nil {
		slog.Warn("container log capture stopped",
			"container", containerName,
			"error", demuxErr,
		)
	} else {
		slog.Warn("container log capture stopped",
			"container", containerName,
		)
	}
}

// processLines reads lines from a reader and emits structured log records.
func (ca *ContainerAggregator) processLines(ctx context.Context, r io.Reader, containerName, stream string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line

	component := "container." + containerName

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		attrs := []any{
			"source", "container",
			"component", component,
			"container", containerName,
			"stream", stream,
		}

		// Try to extract timestamp from container log line
		if ts := extractContainerTS(line); ts != "" {
			attrs = append(attrs, "container_ts", ts)
		}

		// stderr → warn level (per Gemini F5 finding)
		if stream == "stderr" {
			slog.Warn(line, attrs...)
		} else {
			slog.Info(line, attrs...)
		}
	}
}

// resolveContainer resolves a container name to its ID.
func (ca *ContainerAggregator) resolveContainer(ctx context.Context, name string) (string, error) {
	containers, err := ca.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.KeyValuePair{Key: "name", Value: name}),
	})
	if err != nil {
		return "", fmt.Errorf("list containers: %w", err)
	}

	for _, c := range containers {
		for _, n := range c.Names {
			// Docker prefixes names with /
			if strings.TrimPrefix(n, "/") == name {
				return c.ID, nil
			}
		}
	}

	return "", fmt.Errorf("container %q not found", name)
}

// extractContainerTS tries to parse a timestamp from the beginning of a container log line.
var dockerTSRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}[.\d]*(Z|[+-]\d{2}:?\d{2})?)\s`)

func extractContainerTS(line string) string {
	m := dockerTSRe.FindStringSubmatch(line)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

// NewDockerClient creates a real Docker Engine API client.
func NewDockerClient() (DockerClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}
	return cli, nil
}
