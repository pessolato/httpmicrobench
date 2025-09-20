# HTTP Response Body Drain Microbenchmark

This repository provides a microbenchmark suite to measure the impact of draining (reading to EOF) an HTTP response body before closing it in Go HTTP clients. The benchmark launches multiple client and server containers, varying the HTTP protocol version and whether the response body is drained, and collects timing and resource usage statistics.

## Requirements for Running it

- Go 1.25
- Docker Engine 28.4

## Running the Benchmark

To run the benchmark with a custom number of requests (default: 1000):

```sh
NUMBER_OF_REQUESTS=10000 go run ./cmd/bench/
```

This will build the client and server binaries, create Docker images, launch containers, and execute the benchmark. Results will be saved in a timestamped subdirectory under `benchresults/`.

## Summarizing Results

To summarize the results after a benchmark run:

```sh
BENCH_RESULTS_DIRECTORY="benchresults/<timestamp>" go run ./cmd/stats/
```

Replace `<timestamp>` with the actual timestamped directory created by the benchmark (e.g., `20250920150626`). The summary will include request timing statistics and resource usage for each client and server configuration.

## Environment Variables

- `NUMBER_OF_REQUESTS`: Number of requests each client sends (default: 1000).
- `BENCH_RESULTS_DIRECTORY`: Directory containing benchmark results for summary.
- Other variables are set internally by the benchmark runner for container configuration.
