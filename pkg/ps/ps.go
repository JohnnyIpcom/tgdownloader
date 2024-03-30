package ps

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/shirou/gopsutil/v3/process"
)

var proc *process.Process

var (
	unitScales = []int64{
		1000000000000000,
		1000000000000,
		1000000000,
		1000000,
		1000,
	}
)

func formatNumber(value int64, notations map[int64]string) string {
	for _, unitScale := range unitScales {
		if value >= unitScale {
			return fmt.Sprintf("%.2f%s", float64(value)/float64(unitScale), notations[unitScale])
		}
	}
	return fmt.Sprintf("%d%s", value, notations[0])
}

func formatBytes(value int64) string {
	return formatNumber(value, map[int64]string{
		1000000000000000: "PB",
		1000000000000:    "TB",
		1000000000:       "GB",
		1000000:          "MB",
		1000:             "KB",
		0:                "B",
	})
}

func Humanize(ctx context.Context) []string {
	str := make([]string, 0, 3)

	if cpu, err := GetSelfCPU(ctx); err == nil {
		str = append(str, fmt.Sprintf("CPU: %.2f%%", cpu))
	}

	if mem, err := GetSelfMem(ctx); err == nil {
		str = append(str, fmt.Sprintf("Memory: %s", formatBytes(int64(mem.RSS))))
	}

	str = append(str, fmt.Sprintf("Goroutines: %d", GetGoroutineNum()))

	return str
}

func init() {
	var err error
	proc, err = process.NewProcess(int32(os.Getpid()))
	if err != nil {
		panic(err)
	}
}

func GetSelfCPU(ctx context.Context) (float64, error) {
	cpu, err := proc.PercentWithContext(ctx, 0)
	if err != nil {
		return 0, err
	}

	return cpu, nil
}

func GetSelfMem(ctx context.Context) (*process.MemoryInfoStat, error) {
	m, err := proc.MemoryInfoWithContext(ctx)
	if err != nil {
		return nil, err
	}

	return m, nil
}

func GetGoroutineNum() int {
	return runtime.NumGoroutine()
}
