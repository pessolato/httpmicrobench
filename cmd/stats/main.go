package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pessolato/httpmicrobench/pkg/osutil"
)

type logEntry struct {
	Time        time.Time `json:"time"`
	Level       string    `json:"level"`
	Msg         string    `json:"msg"`
	Port        string    `json:"port,omitempty"`
	ReqUUID     string    `json:"req_uuid"`
	Host        string    `json:"host,omitempty"`
	Network     string    `json:"network,omitempty"`
	Addr        string    `json:"addr,omitempty"`
	Reused      bool      `json:"reused,omitempty"`
	Status      bool      `json:"status,omitempty"`
	StatusCode  int       `json:"status_code,omitempty"`
	MaxTimeNano int64     `json:"max_time_nano,omitempty"`
}

type statEntry struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage int64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage int64 `json:"system_cpu_usage"`
		OnlineCpus     int64 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PrecpuStats struct {
		CPUUsage struct {
			TotalUsage int64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage int64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
}

func main() {
	benchResDir := ""
	osutil.ExitOnErr(
		osutil.Load(
			osutil.NewEnvVar("BENCH_RESULTS_DIRECTORY", &benchResDir, true),
		))

	osutil.ExitOnErr(
		filepath.WalkDir(benchResDir, func(path string, d fs.DirEntry, err error) error {
			if d.IsDir() {
				return nil
			}

			if strings.Contains(path, "logs.jsonl") {
				printLogSummary(path)
				return nil
			}
			if strings.Contains(path, "stats.jsonl") {
				printStatSummary(path)
				return nil
			}

			return nil
		}),
	)

}

func printLogSummary(path string) {
	fmt.Printf("Summarizing result logs from file: %s\n", path)
	f, err := os.Open(path)
	osutil.ExitOnErr(err)
	defer f.Close()

	var reqTimesNano []int64
	scn := bufio.NewScanner(f)
	for scn.Scan() {
		var e logEntry
		err := json.Unmarshal(scn.Bytes(), &e)
		osutil.ExitOnErr(err)

		if e.MaxTimeNano == 0 {
			continue
		}
		reqTimesNano = append(reqTimesNano, e.MaxTimeNano)
	}
	osutil.ExitOnErr(scn.Err())
	min, max, mean, median := summarizeStats(reqTimesNano)
	fmt.Printf(
		"Request Time:\n- Min: %s\n- Max: %s\n- Mean: %s\n- Median: %s\n\n",
		time.Duration(min),
		time.Duration(max),
		time.Duration(mean),
		time.Duration(median),
	)
}

func printStatSummary(path string) {
	fmt.Printf("Summarizing result stats from file: %s\n", path)
	f, err := os.Open(path)
	osutil.ExitOnErr(err)
	defer f.Close()

	var cpuRecordings []float64
	scn := bufio.NewScanner(f)
	for scn.Scan() {
		var e statEntry
		err := json.Unmarshal(scn.Bytes(), &e)
		osutil.ExitOnErr(err)
		cpuDelta := e.CPUStats.CPUUsage.TotalUsage - e.PrecpuStats.CPUUsage.TotalUsage
		sysCpuDelta := e.CPUStats.SystemCPUUsage - e.PrecpuStats.SystemCPUUsage

		if sysCpuDelta == 0 || e.CPUStats.OnlineCpus == 0 {
			continue
		}

		numCpu := e.CPUStats.OnlineCpus
		cpuUsage := (float64(cpuDelta) / float64(sysCpuDelta)) * float64(numCpu) * 100
		cpuRecordings = append(cpuRecordings, cpuUsage)
	}
	osutil.ExitOnErr(scn.Err())
	min, max, mean, median := summarizeStats(cpuRecordings)
	fmt.Printf(
		"CPU Usage:\n- Min: %.2f%%\n- Max: %.2f%%\n- Mean: %.2f%%\n- Median: %.2f%%\n\n",
		min,
		max,
		mean,
		median,
	)
}

type number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

func summarizeStats[T number](stats []T) (min, max, mean, median T) {
	if len(stats) < 1 {
		return
	}

	slices.Sort(stats)
	var sum T
	for i, t := range stats {
		sum += t
		mean = sum / T(i+1)

		if t > max {
			max = t
		}

		if min == 0 || t < min {
			min = t
		}
	}

	l := len(stats)
	if l%2 == 1 {
		median = stats[l/2]
	} else {
		median = (stats[(l/2)-1] + stats[l/2]) / 2
	}
	return
}
