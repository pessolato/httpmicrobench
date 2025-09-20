package orchestration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/pessolato/httpmicrobench/pkg/osutil"

	"github.com/docker/docker/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type RunStep func(context.Context, *client.Client) error

type DockerOrchestrator struct {
	pre, run, pos []RunStep
	// c is the Docker SDK client used for all operations.
	c *client.Client
}

func NewDockerOrchestrator() (*DockerOrchestrator, error) {
	c, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &DockerOrchestrator{c: c}, nil
}

// WithPreRunStep sets the pre-run steps.
//
// Failures during pre-run steps halt the process
// and do not execute any other phases of the orchestration.
func (o *DockerOrchestrator) WithPreRunStep(steps ...RunStep) *DockerOrchestrator {
	o.pre = append(o.pre, steps...)
	return o
}

// WithPreRunStep sets the run steps.
//
// Failures during run steps skips to the post-run part.
func (o *DockerOrchestrator) WithRunStep(steps ...RunStep) *DockerOrchestrator {
	o.run = append(o.run, steps...)
	return o
}

// WithPosRunStep sets the post-run steps.
//
// Failures during post-run steps halt the process.
func (o *DockerOrchestrator) WithPosRunStep(steps ...RunStep) *DockerOrchestrator {
	o.pos = append(o.pos, steps...)
	return o
}

func (o *DockerOrchestrator) Run(ctx context.Context) error {
	for _, s := range o.pre {
		if err := s(ctx, o.c); err != nil {
			return fmt.Errorf("failed running pre run step: %w", err)
		}
	}

	var runErr error
	for _, s := range o.run {
		if err := s(ctx, o.c); err != nil {
			runErr = fmt.Errorf("failed running step: %w", err)
			break
		}
	}

	for _, s := range o.pos {
		if err := s(ctx, o.c); err != nil {
			runErr = errors.Join(fmt.Errorf("failed running pos run step: %w", err), runErr)
			break
		}
	}

	return runErr
}

type Container struct {
	Name     string
	Config   container.Config
	Network  network.NetworkingConfig
	LogSink  io.WriteCloser
	StatSink io.WriteCloser
	// ID is usually used as a read-only field which
	// is populated when a create step is executed.
	ID string
}

func ContainerCreateStep(specs ...*Container) RunStep {
	return func(ctx context.Context, c *client.Client) error {
		for _, s := range specs {
			resp, err := c.ContainerCreate(ctx, &s.Config, nil, &s.Network, nil, s.Name)
			if err != nil {
				return fmt.Errorf("failed to create %s container: %w", s.Name, err)
			}
			s.ID = resp.ID
		}
		return nil
	}
}

func ContainerStartStep(specs ...*Container) RunStep {
	return func(ctx context.Context, c *client.Client) error {
		for _, s := range specs {
			if err := c.ContainerStart(ctx, s.ID, client.ContainerStartOptions{}); err != nil {
				return fmt.Errorf("failed to start %s container: %w", s.Name, err)
			}
		}
		return nil
	}
}

// ContainerLogStep returns a RunStep that copies the container logs
// to the provided log sinks concurrently in the background.
//
// Only logs of Containers with a non-nil LogSink are copied.
func ContainerLogStep(errLogSink io.Writer, specs ...*Container) RunStep {
	return func(ctx context.Context, c *client.Client) error {
		for _, s := range specs {
			if s.LogSink == nil {
				// If the container does not have a log sink, skip the collection for it.
				continue
			}

			in, err := c.ContainerLogs(ctx, s.ID,
				client.ContainerLogsOptions{
					ShowStdout: true,
					ShowStderr: true,
					Follow:     true,
				})
			if err != nil {
				return fmt.Errorf("failed to get logs for %s container: %w", s.Name, err)
			}

			go func(cnt *Container) {
				_, err := stdcopy.StdCopy(cnt.LogSink, errLogSink, in)
				err = errors.Join(err, in.Close(), cnt.LogSink.Close())
				if err != nil {
					fmt.Fprintln(errLogSink, fmt.Errorf("failed to copy %s container logs or close sinks: %w", cnt.Name, err))
				}
			}(s)
		}

		return nil
	}
}

// ContainerStreamStatStep returns a RunStep that copies the container stats
// to the provided metric sinks concurrently in the background.
//
// Only stats of Containers with a non-nil StatSink are copied.
func ContainerStreamStatStep(errLogSink io.Writer, specs ...*Container) RunStep {
	return func(ctx context.Context, c *client.Client) error {
		for _, s := range specs {
			if s.StatSink == nil {
				// If the container does not have a metric sink, skip the collection for it.
				continue
			}

			r, err := c.ContainerStats(ctx, s.ID, true)
			if err != nil {
				return fmt.Errorf("failed to get %s container stats: %w", s.Name, err)
			}

			go func(cnt *Container) {
				_, err := io.Copy(cnt.StatSink, r.Body)
				err = errors.Join(err, r.Body.Close(), cnt.StatSink.Close())
				if err != nil {
					fmt.Fprintln(errLogSink, fmt.Errorf("failed to copy %s container stats or close sinks: %w", s.Name, err))
				}
			}(s)

		}
		return nil
	}
}

func ContainerWaitStep(errLogSink io.Writer, specs ...*Container) RunStep {
	return func(ctx context.Context, c *client.Client) error {
		var wg sync.WaitGroup
		for _, s := range specs {
			stsCh, errCh := c.ContainerWait(ctx, s.ID, container.WaitConditionNotRunning)
			wg.Add(1)
			go func(stsCh <-chan container.WaitResponse, errCh <-chan error) {
				defer wg.Done()
				select {
				case err := <-errCh:
					if err != nil {
						fmt.Fprintln(errLogSink, err)
					}
				case <-stsCh:
				}
			}(stsCh, errCh)
		}

		wg.Wait()
		return nil
	}
}

func ContainerStopStep(specs ...*Container) RunStep {
	return func(ctx context.Context, c *client.Client) error {
		for _, s := range specs {
			err := c.ContainerStop(ctx, s.ID, client.ContainerStopOptions{})
			if err != nil {
				return fmt.Errorf("failed to stop %s container: %w", s.Name, err)
			}
		}
		return nil
	}
}

func ContainerRemoveStep(specs ...*Container) RunStep {
	return func(ctx context.Context, c *client.Client) error {
		for _, s := range specs {
			err := c.ContainerRemove(ctx, s.ID, client.ContainerRemoveOptions{})
			if err != nil {
				return fmt.Errorf("failed to remove %s container: %w", s.Name, err)
			}
		}
		return nil
	}
}

func EnsureContainerSinkCloseStep(specs ...*Container) RunStep {
	return func(ctx context.Context, c *client.Client) error {
		for _, s := range specs {
			if s.LogSink != nil {
				s.LogSink.Close()
			}
			if s.StatSink != nil {
				s.StatSink.Close()
			}
		}
		return nil
	}
}

type Network struct {
	// Name is the network name used for the network creationg
	Name string
	// ID is usually used as a read-only field which
	// is populated when a create step is executed.
	ID string
}

func EnsureNetworkStep(specs ...*Network) RunStep {
	return func(ctx context.Context, c *client.Client) error {
		if len(specs) < 1 {
			return nil
		}

		nets, err := c.NetworkList(ctx, client.NetworkListOptions{})
		if err != nil {
			return fmt.Errorf("failed listing networks: %w", err)
		}

		names := networkNameSet(nets)
		for _, s := range specs {
			if _, ok := names[s.Name]; ok {
				continue
			}

			resp, err := c.NetworkCreate(ctx, s.Name, client.NetworkCreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create %s network: %w", s.Name, err)
			}

			s.ID = resp.ID
		}
		return nil
	}
}

type GoBuild struct {
	PkgPath, Dest string
	BuildCtxSpecs []osutil.BuildCtxSpec
	// ArtifactStore is used to store the context once the build is complete.
	ArtifactStore io.Writer
}

func GoBuildStep(specs ...*GoBuild) RunStep {
	return func(ctx context.Context, c *client.Client) error {
		for _, s := range specs {
			err := osutil.BuildGo(s.Dest, s.PkgPath)
			if err != nil {
				return fmt.Errorf("failed building %s package: %w", s.PkgPath, err)
			}

			r, err := osutil.BuildCtx(s.BuildCtxSpecs...)
			if err != nil {
				return fmt.Errorf("failed building artifacts for %s package: %w", s.PkgPath, err)
			}

			_, err = io.Copy(s.ArtifactStore, r)
			if err != nil {
				return fmt.Errorf("failed storing artifacts for %s package: %w", s.PkgPath, err)
			}
		}
		return nil
	}
}

type Image struct {
	Tag      string
	Rebuild  bool
	BuildCtx io.Reader
}

func EnsureImageStep(specs ...*Image) RunStep {
	return func(ctx context.Context, c *client.Client) error {
		if len(specs) < 1 {
			return nil
		}

		res, err := c.ImageList(ctx, client.ImageListOptions{})
		if err != nil {
			return fmt.Errorf("failed listing images: %w", err)
		}

		tags := imageTagSet(res)
		for _, s := range specs {
			if _, ok := tags[s.Tag]; !ok || s.Rebuild {
				resp, err := c.ImageBuild(ctx, s.BuildCtx, client.ImageBuildOptions{Tags: []string{s.Tag}, Remove: true})
				if err := osutil.DrainCloseErr(resp.Body, err); err != nil {
					return fmt.Errorf("failed building image %s: %w", s.Tag, err)
				}
			}
		}

		return nil
	}
}

func imageTagSet(imgs []image.Summary) map[string]struct{} {
	tags := make(map[string]struct{})
	for _, i := range imgs {
		for _, t := range i.RepoTags {
			tags[t] = struct{}{}
		}
	}
	return tags
}

func networkNameSet(nets []network.Summary) map[string]struct{} {
	names := make(map[string]struct{})
	for _, n := range nets {
		names[n.Name] = struct{}{}
	}
	return names
}
