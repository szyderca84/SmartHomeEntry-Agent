// Package metrics collects lightweight CPU and RAM usage from /proc on Linux.
// It is designed to add minimal overhead to the heartbeat cycle.
package metrics

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Sample holds a single CPU/RAM snapshot.
type Sample struct {
	CPUPercent float64
	RAMPercent float64
	RAMUsedMB  int
	RAMTotalMB int
}

// Collect reads CPU and RAM metrics from /proc/stat and /proc/meminfo.
// CPU utilisation is computed from two /proc/stat samples taken 1 second apart,
// giving a meaningful average rather than an instantaneous point reading.
// The context is honoured; if it is cancelled during the 1-second sleep the
// function returns early with a partial error.
func Collect(ctx context.Context) (*Sample, error) {
	// First CPU snapshot
	idle0, total0, err := readCPUStat()
	if err != nil {
		return nil, fmt.Errorf("metrics: first cpu sample: %w", err)
	}

	// 1-second sleep (respects context cancellation)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(time.Second):
	}

	// Second CPU snapshot
	idle1, total1, err := readCPUStat()
	if err != nil {
		return nil, fmt.Errorf("metrics: second cpu sample: %w", err)
	}

	var cpuPercent float64
	deltaTotal := total1 - total0
	deltaIdle := idle1 - idle0
	if deltaTotal > 0 {
		cpuPercent = (float64(deltaTotal-deltaIdle) / float64(deltaTotal)) * 100.0
	}

	// RAM
	memTotal, memAvail, err := readMemInfo()
	if err != nil {
		return nil, fmt.Errorf("metrics: meminfo: %w", err)
	}

	var ramPercent float64
	if memTotal > 0 {
		ramPercent = float64(memTotal-memAvail) / float64(memTotal) * 100.0
	}
	ramUsedMB := (memTotal - memAvail) / 1024
	ramTotalMB := memTotal / 1024

	return &Sample{
		CPUPercent: cpuPercent,
		RAMPercent: ramPercent,
		RAMUsedMB:  ramUsedMB,
		RAMTotalMB: ramTotalMB,
	}, nil
}

// readCPUStat reads the first "cpu" line from /proc/stat and returns
// (idleJiffies, totalJiffies).
func readCPUStat() (idle, total uint64, err error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		// Fields: cpu user nice system idle iowait irq softirq steal guest guest_nice
		if len(fields) < 5 {
			return 0, 0, fmt.Errorf("unexpected /proc/stat format: %q", line)
		}
		var vals [10]uint64
		for i := 1; i < len(fields) && i <= 10; i++ {
			v, parseErr := strconv.ParseUint(fields[i], 10, 64)
			if parseErr != nil {
				return 0, 0, fmt.Errorf("parse /proc/stat field %d: %w", i, parseErr)
			}
			vals[i-1] = v
			total += v
		}
		// idle = idle + iowait (index 3 and 4)
		idle = vals[3] + vals[4]
		return idle, total, nil
	}
	return 0, 0, fmt.Errorf("/proc/stat: cpu line not found")
}

// readMemInfo reads MemTotal and MemAvailable from /proc/meminfo (values in kB).
func readMemInfo() (memTotal, memAvail int, err error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	found := 0
	for scanner.Scan() && found < 2 {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, parseErr := strconv.Atoi(fields[1])
		if parseErr != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			memTotal = v
			found++
		case "MemAvailable:":
			memAvail = v
			found++
		}
	}
	if memTotal == 0 {
		return 0, 0, fmt.Errorf("/proc/meminfo: MemTotal not found")
	}
	return memTotal, memAvail, nil
}
