package my_collectors

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"log/slog"

	"github.com/jaypipes/ghw"
	"github.com/prometheus-community/windows_exporter/internal/mi"
	"github.com/prometheus-community/windows_exporter/pkg/collector"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/mem"
)

// =============================
// 硬件信息缓存结构体
// =============================
var hardwareInfoCache = struct {
	LastScan time.Time
	Data     *HardwareInfo
	Mutex    sync.RWMutex
}{Data: nil}

// 缓存刷新周期，默认8小时，可通过环境变量 HARDWARE_INFO_INTERVAL 配置
var hardwareInfoInterval = func() time.Duration {
	if v := os.Getenv("HARDWARE_INFO_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return 8 * time.Hour
}()

// 操作调用超时，默认10秒，可通过环境变量 HARDWARE_INFO_TIMEOUT 配置
var hardwareInfoTimeout = func() time.Duration {
	if v := os.Getenv("HARDWARE_INFO_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 10 * time.Second
}()

// =============================
// 带超时的辅助函数（用于不支持 context 的库调用）
// =============================

func getProductWithTimeout(ctx context.Context) (*ghw.ProductInfo, error) {
	resultCh := make(chan struct{})
	var (
		res *ghw.ProductInfo
		err error
	)
	go func() {
		res, err = ghw.Product()
		close(resultCh)
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-resultCh:
		return res, err
	}
}

func getBaseboardWithTimeout(ctx context.Context) (*ghw.BaseboardInfo, error) {
	resultCh := make(chan struct{})
	var (
		res *ghw.BaseboardInfo
		err error
	)
	go func() {
		res, err = ghw.Baseboard()
		close(resultCh)
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-resultCh:
		return res, err
	}
}

func getPlatformInformationWithTimeout(ctx context.Context) (string, string, string, error) {
	resultCh := make(chan struct{})
	var (
		name    string
		family  string
		version string
		err     error
	)
	go func() {
		name, family, version, err = host.PlatformInformation()
		close(resultCh)
	}()
	select {
	case <-ctx.Done():
		return "", "", "", ctx.Err()
	case <-resultCh:
		return name, family, version, err
	}
}

// =============================
// 硬件信息结构体
// =============================
type HardwareInfo struct {
	BoardVendor   string
	BoardProduct  string
	BoardSerial   string
	BoardVersion  string
	SystemVendor  string
	SystemProduct string
	SystemSerial  string
	SystemUUID    string
	CPUModel      string
	CPUCores      int
	MemoryTotal   float64 // 单位: 字节
	DiskTotal     float64 // 单位: 字节
	OSName        string
	OSType        string
	OSVersion     string
}

// =============================
// 获取硬件信息，带缓存，每8小时刷新一次
// =============================
func getHardwareInfo() *HardwareInfo {
	hardwareInfoCache.Mutex.RLock()
	expired := time.Since(hardwareInfoCache.LastScan) > hardwareInfoInterval || hardwareInfoCache.Data == nil
	hardwareInfoCache.Mutex.RUnlock()
	if expired {
		hardwareInfoCache.Mutex.Lock()
		if time.Since(hardwareInfoCache.LastScan) > hardwareInfoInterval || hardwareInfoCache.Data == nil {
			hardwareInfoCache.Data = collectHardwareInfo()
			hardwareInfoCache.LastScan = time.Now()
		}
		hardwareInfoCache.Mutex.Unlock()
	}
	hardwareInfoCache.Mutex.RLock()
	defer hardwareInfoCache.Mutex.RUnlock()
	return hardwareInfoCache.Data
}

// =============================
// 真正采集硬件信息的函数
// =============================
func collectHardwareInfo() *HardwareInfo {
    ctx, cancel := context.WithTimeout(context.Background(), hardwareInfoTimeout)
    defer cancel()
	info := &HardwareInfo{
		BoardVendor:   "未知",
		BoardProduct:  "未知",
		BoardSerial:   "未知",
		BoardVersion:  "未知",
		SystemVendor:  "未知",
		SystemProduct: "未知",
		SystemSerial:  "未知",
		SystemUUID:    "未知",
		CPUModel:      "未知",
		CPUCores:      0,
		MemoryTotal:   0,
		DiskTotal:     0,
		OSName:        "未知",
		OSType:        "未知",
		OSVersion:     "未知",
	}
	// 获取主板和系统信息（带超时保护）
	if p, err := getProductWithTimeout(ctx); err == nil && p != nil {
		if p.Vendor != "" {
			info.SystemVendor = p.Vendor
		}
		if p.Name != "" {
			info.SystemProduct = p.Name
		}
		if p.SerialNumber != "" {
			info.SystemSerial = p.SerialNumber
		}
		if p.UUID != "" {
			info.SystemUUID = p.UUID
		}
	}
	if b, err := getBaseboardWithTimeout(ctx); err == nil && b != nil {
		if b.Vendor != "" {
			info.BoardVendor = b.Vendor
		}
		if b.Product != "" {
			info.BoardProduct = b.Product
		}
		if b.SerialNumber != "" {
			info.BoardSerial = b.SerialNumber
		}
		if b.Version != "" {
			info.BoardVersion = b.Version
		}
	}
	// 获取CPU信息（带超时）
	if c, err := cpu.InfoWithContext(ctx); err == nil && len(c) > 0 {
		if c[0].ModelName != "" {
			info.CPUModel = c[0].ModelName
		}
		info.CPUCores = len(c)
	}
	// 获取内存信息（带超时）
	if m, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		info.MemoryTotal = float64(m.Total)
	}
	// 获取磁盘总容量（仅统计本地固定磁盘，带超时）
	diskTotal := float64(0)
	if parts, err := disk.PartitionsWithContext(ctx, false); err == nil {
		for _, part := range parts {
			if ctx.Err() != nil {
				break
			}
			// 仅统计固定本地盘，跳过网络盘、光驱、可移动盘
			opts := strings.ToLower(part.Opts)
			if strings.Contains(opts, "remote") || strings.Contains(opts, "cdrom") || strings.Contains(opts, "removable") {
				continue
			}
			if d, err := disk.UsageWithContext(ctx, part.Mountpoint); err == nil {
				diskTotal += float64(d.Total)
			}
		}
	}
	info.DiskTotal = diskTotal
	// 获取操作系统信息（避免调用 Info() 触发进程枚举，改用 PlatformInformation，带超时）
	if name, family, version, err := getPlatformInformationWithTimeout(ctx); err == nil {
		if name != "" {
			info.OSName = name
		}
		if family != "" {
			info.OSType = family
		}
		if version != "" {
			info.OSVersion = version
		}
	}
	return info
}

// =============================
// 单位自动转换
// =============================
func convertSize(size float64) (float64, string) {
	var unit string
	var convertedSize float64
	switch {
	case size >= 1024*1024*1024*1024:
		convertedSize = size / (1024 * 1024 * 1024 * 1024)
		unit = "TB"
	case size >= 1024*1024*1024:
		convertedSize = size / (1024 * 1024 * 1024)
		unit = "GB"
	case size >= 1024*1024:
		convertedSize = size / (1024 * 1024)
		unit = "MB"
	default:
		convertedSize = size / 1024
		unit = "KB"
	}
	return convertedSize, unit
}

// =============================
// 硬件信息采集器结构体
// =============================
type HardwareInfoCollector struct {
	boardInfoDesc   *prometheus.Desc
	systemInfoDesc  *prometheus.Desc
	cpuInfoDesc     *prometheus.Desc
	memoryTotalDesc *prometheus.Desc
	diskTotalDesc   *prometheus.Desc
	osInfoDesc      *prometheus.Desc
}

// =============================
// 采集器构造函数
// =============================
func NewHardwareInfoCollector() *HardwareInfoCollector {
	return &HardwareInfoCollector{
		boardInfoDesc: prometheus.NewDesc(
			"node_hardware_board_info",
			"主板信息（厂商、型号、序列号、版本）",
			[]string{"vendor", "product", "serial", "version"}, nil,
		),
		systemInfoDesc: prometheus.NewDesc(
			"node_hardware_system_info",
			"系统信息（厂商、型号、序列号、UUID）",
			[]string{"vendor", "product", "serial", "uuid"}, nil,
		),
		cpuInfoDesc: prometheus.NewDesc(
			"node_hardware_cpu_info",
			"CPU信息（型号、核心数）",
			[]string{"model"}, nil,
		),
		memoryTotalDesc: prometheus.NewDesc(
			"node_hardware_memory_total_bytes",
			"内存总容量（单位：字节）",
			[]string{"display"}, nil,
		),
		diskTotalDesc: prometheus.NewDesc(
			"node_hardware_disk_total_bytes",
			"磁盘总容量（单位：字节）",
			[]string{"display"}, nil,
		),
		osInfoDesc: prometheus.NewDesc(
			"node_hardware_os_info",
			"操作系统信息（名称、类型、版本）",
			[]string{"name", "type", "version"}, nil,
		),
	}
}

// =============================
// Describe 方法
// =============================
func (c *HardwareInfoCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.boardInfoDesc
	ch <- c.systemInfoDesc
	ch <- c.cpuInfoDesc
	ch <- c.memoryTotalDesc
	ch <- c.diskTotalDesc
	ch <- c.osInfoDesc
}

// =============================
// Collector 接口实现
// =============================

// GetName 返回采集器名称
func (c *HardwareInfoCollector) GetName() string {
	return "hardware_info"
}

// Build 初始化采集器（如无特殊需求可直接返回 nil）
func (c *HardwareInfoCollector) Build(logger *slog.Logger, miSession *mi.Session) error {
	return nil
}

// Collect 采集指标并输出到 Prometheus
func (c *HardwareInfoCollector) Collect(ch chan<- prometheus.Metric) error {
	info := getHardwareInfo()
	// 主板信息
	ch <- prometheus.MustNewConstMetric(
		c.boardInfoDesc, prometheus.GaugeValue, 1,
		info.BoardVendor, info.BoardProduct, info.BoardSerial, info.BoardVersion,
	)
	// 系统信息
	ch <- prometheus.MustNewConstMetric(
		c.systemInfoDesc, prometheus.GaugeValue, 1,
		info.SystemVendor, info.SystemProduct, info.SystemSerial, info.SystemUUID,
	)
	// CPU信息
	ch <- prometheus.MustNewConstMetric(
		c.cpuInfoDesc, prometheus.GaugeValue, float64(info.CPUCores),
		info.CPUModel,
	)
	// 内存总容量
	memVal, memUnit := convertSize(info.MemoryTotal)
	ch <- prometheus.MustNewConstMetric(
		c.memoryTotalDesc, prometheus.GaugeValue, info.MemoryTotal,
		fmt.Sprintf("%.0f %s", math.Round(memVal), memUnit),
	)
	// 磁盘总容量
	diskVal, diskUnit := convertSize(info.DiskTotal)
	ch <- prometheus.MustNewConstMetric(
		c.diskTotalDesc, prometheus.GaugeValue, info.DiskTotal,
		fmt.Sprintf("%.0f %s", math.Round(diskVal), diskUnit),
	)
	// 操作系统信息
	ch <- prometheus.MustNewConstMetric(
		c.osInfoDesc, prometheus.GaugeValue, 1,
		info.OSName, info.OSType, info.OSVersion,
	)
	return nil
}

// Close 关闭采集器（如无特殊需求可直接返回 nil）
func (c *HardwareInfoCollector) Close() error {
	return nil
}

func init() {
	collector.RegisterCollector("hardware_info", func() collector.Collector {
		return NewHardwareInfoCollector()
	})
}
