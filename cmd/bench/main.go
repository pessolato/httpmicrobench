package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pessolato/httpmicrobench/pkg/orchestration"
	"github.com/pessolato/httpmicrobench/pkg/osutil"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

const (
	netName     = "http-bench-network"
	clientRsrc  = "client"
	serverRsrc  = "server"
	imgTag      = ":latest"
	goBuildDest = "./build/bin/"
	pkgBasePath = "./cmd/"

	clientImg         = clientRsrc + imgTag
	clientPkgPath     = pkgBasePath + clientRsrc + "/"
	clientGoBuildDest = goBuildDest + clientRsrc
	serverImg         = serverRsrc + imgTag
	serverPkgPath     = pkgBasePath + serverRsrc + "/"
	serverGoBuildDest = goBuildDest + serverRsrc

	// totalContainers is the total containers the test will create.
	//
	// 4 clients for each combination of HTTP version and whether to drain
	// the response body before closing it or not.
	//
	// 2 servers to measure stats on the server when body is drained or not.
	totalContainers = 6
)

func main() {
	resourcePrefix := ""
	numOfReqs := 1000
	responseLength := 1000
	forceRebuild := false
	outputDir := "benchresults"

	osutil.ExitOnErr(
		osutil.Load(
			osutil.NewEnvVar("RESOURCE_PREFIX", &resourcePrefix, false),
			osutil.NewEnvVar("NUMBER_OF_REQUESTS", &numOfReqs, false),
			osutil.NewEnvVar("RESPONSE_LENGTH", &responseLength, false),
			osutil.NewEnvVar("FORCE_IMAGE_REBUILD", &forceRebuild, false),
			osutil.NewEnvVar("OUTPUT_DIRECTORY", &outputDir, false),
		))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	testRunTs := time.Now().Format("20060102150405")

	var clientBuildCtxBuf, serverBuildCtxBuf bytes.Buffer
	var clientImgSpec, serverImgSpec orchestration.Image
	var benchNetwork orchestration.Network
	containers := make([]*orchestration.Container, totalContainers)
	orch, err := orchestration.NewDockerOrchestrator()
	osutil.ExitOnErr(err)

	osutil.ExitOnErr(
		orch.WithPreRunStep(
			// Define required pre-run artifacts.
			func(ctx context.Context, c *client.Client) error {
				// HTTP Client Image Specification
				clientImgSpec = orchestration.Image{
					Tag:      resourcePrefix + clientImg,
					Rebuild:  forceRebuild,
					BuildCtx: &clientBuildCtxBuf,
				}
				// HTTP Server Image Specification
				serverImgSpec = orchestration.Image{
					Tag:      resourcePrefix + serverImg,
					Rebuild:  forceRebuild,
					BuildCtx: &serverBuildCtxBuf,
				}
				// Docker Network Specification
				benchNetwork = orchestration.Network{
					Name: resourcePrefix + netName,
				}
				return nil
			},
			orchestration.GoBuildStep(
				// Build client binary
				&orchestration.GoBuild{
					PkgPath:       clientPkgPath,
					Dest:          clientGoBuildDest,
					BuildCtxSpecs: buildCtxSpecs(clientGoBuildDest),
					ArtifactStore: &clientBuildCtxBuf,
				},
				// Build server binary
				&orchestration.GoBuild{
					PkgPath:       serverPkgPath,
					Dest:          serverGoBuildDest,
					BuildCtxSpecs: buildCtxSpecs(serverGoBuildDest),
					ArtifactStore: &serverBuildCtxBuf,
				},
			),
			orchestration.EnsureImageStep(&clientImgSpec, &serverImgSpec),
			orchestration.EnsureNetworkStep(&benchNetwork),
		).
			WithRunStep(
				// Define run artifacts
				func(ctx context.Context, c *client.Client) error {
					outDir := filepath.Join(outputDir, testRunTs)
					err := os.MkdirAll(outDir, os.ModePerm)
					if err != nil {
						return fmt.Errorf("error to create logs dir: %w", err)
					}
					// Must create one container for each option
					// HTTP version + drain response body or not.
					httpVersions := []int{1, 2, 1, 2}
					drainSettings := []int{1, 1, 0, 0}
					for i := range totalContainers - 2 {
						name := fmt.Sprintf("%s-http-%d-drain-%d", clientRsrc, httpVersions[i], drainSettings[i])
						logF, err := os.Create(filepath.Join(outDir, name+"-logs.jsonl"))
						if err != nil {
							return fmt.Errorf("error to create log file for %s container: %w", name, err)
						}
						statF, err := os.Create(filepath.Join(outDir, name+"-stats.jsonl"))
						if err != nil {
							return fmt.Errorf("error to create log file for %s container: %w", name, err)
						}
						containers[i] = &orchestration.Container{
							Name: name,
							Config: container.Config{
								Image: clientImg,
								Env: []string{
									fmt.Sprintf("TARGET_ENDPOINT_URI=http://%s-%d:8080/%d", serverRsrc, drainSettings[i], responseLength),
									fmt.Sprintf("CLIENT_HTTP_VERSION=%d", httpVersions[i]),
									fmt.Sprintf("MUST_DRAIN_AND_CLOSE=%d", drainSettings[i]),
									fmt.Sprintf("NUMBER_OF_REQUESTS=%d", numOfReqs),
								},
							},
							Network: network.NetworkingConfig{
								EndpointsConfig: endpointConfig(benchNetwork),
							},
							LogSink:  logF,
							StatSink: statF,
						}

					}
					// Must create 1 server for handling requests from clients that will not
					// drain the response body, and another for clinets that will.
					for i := range 2 {
						statF, err := os.Create(filepath.Join(outDir, fmt.Sprintf("server-drain-%d-stats.jsonl", i)))
						if err != nil {
							return fmt.Errorf("error to create stat file for server container: %w", err)
						}
						containers[totalContainers-1-i] = &orchestration.Container{
							Name: fmt.Sprintf("%s-%d", serverRsrc, i),
							Config: container.Config{
								Image: serverImg,
							},
							Network: network.NetworkingConfig{
								EndpointsConfig: endpointConfig(benchNetwork),
							},
							StatSink: statF,
						}
					}
					return nil
				},
				orchestration.ContainerCreateStep(containers...),
				orchestration.ContainerStreamStatStep(os.Stderr, containers...),
				orchestration.ContainerStartStep(containers...),
				orchestration.ContainerLogStep(os.Stderr, containers...),
				// Wait only for the client containers.
				orchestration.ContainerWaitStep(os.Stderr, containers[:totalContainers-2]...),
			).
			WithPosRunStep(
				orchestration.ContainerStopStep(containers...),
				orchestration.ContainerRemoveStep(containers...),
				orchestration.EnsureContainerSinkCloseStep(containers...),
			).
			Run(ctx),
	)

}

func buildCtxSpecs(binPath string) []osutil.BuildCtxSpec {
	return []osutil.BuildCtxSpec{
		{FineName: "app", PathTo: binPath, Mode: 0555},
		{FineName: "Dockerfile", PathTo: "./build/Dockerfile", Mode: 0444},
	}
}

func endpointConfig(n orchestration.Network) map[string]*network.EndpointSettings {
	return map[string]*network.EndpointSettings{
		n.Name: {
			NetworkID: n.ID,
		},
	}
}
