# hardware_info_collector 采集器文档

## 功能简介

`hardware_info_collector` 是一个适用于 windows_exporter 的自定义 Prometheus 采集器，用于采集主机的硬件和操作系统基础信息。支持自动注册，便于批量纳管和自动化部署。

## 采集内容

- 主板信息（厂商、型号、序列号、版本）
- 系统信息（厂商、型号、序列号、UUID）
- CPU 信息（型号、核心数）
- 内存总容量（字节）
- 磁盘总容量（字节，自动合计所有本地盘）
- 操作系统信息（名称、类型、版本）

## 指标说明

| 指标名                          | 类型   | 标签/值                | 说明                     |
| ------------------------------- | ------ | ---------------------- | ------------------------ |
| node_hardware_board_info        | Gauge  | vendor,product,serial,version | 主板信息         |
| node_hardware_system_info       | Gauge  | vendor,product,serial,uuid    | 系统信息         |
| node_hardware_cpu_info          | Gauge  | model                  | CPU核心数（value为核心数）|
| node_hardware_memory_total_bytes| Gauge  | display                | 内存总容量（字节）        |
| node_hardware_disk_total_bytes  | Gauge  | display                | 磁盘总容量（字节）        |
| node_hardware_os_info           | Gauge  | name,type,version      | 操作系统信息             |

- 字符串信息以 label 形式暴露，数值信息以 value 形式暴露。
- 采集失败时，label 为 "未知"，value 为 0。

## 缓存机制与性能说明

- **缓存结构体**：采集结果缓存在内存中（`hardwareInfoCache`），只保留一份最新的 `HardwareInfo`。
- **刷新周期**：默认每8小时刷新一次（可通过环境变量 `HARDWARE_INFO_INTERVAL` 配置，如 `2h`、`30m`）。
- **采集流程**：
  1. Prometheus 拉取指标时，`Collect` 方法调用 `getHardwareInfo()`。
  2. 若距离上次采集未超过刷新周期，则直接返回缓存数据。
  3. 若已过期，则重新采集硬件信息并更新缓存。
- **并发安全**：所有缓存读写均加锁（`sync.RWMutex`），多线程安全。
- **性能优势**：绝大多数 scrape（如每30秒/1分钟）都只读缓存，极高性能，仅每8小时（或自定义周期）才真正采集一次。
- **内存占用**：只保留一份最新采集结果，无内存泄漏风险。

## 环境依赖

- Go 1.18 及以上
- 依赖三方库：
  - [github.com/jaypipes/ghw](https://github.com/jaypipes/ghw)
  - [github.com/shirou/gopsutil](https://github.com/shirou/gopsutil)
  - [github.com/prometheus/client_golang/prometheus](https://github.com/prometheus/client_golang)

## 使用方法

1. **放置采集器文件**
   - 将 `hardware_info_collector.go` 放入 `my_collectors` 目录。

2. **自动注册**
   - 在 `my_collectors` 目录下运行：
     ```bash
     bash auto_register_collectors.sh
     ```
   - 脚本会自动将采集器复制到 `../windows_exporter/my_collectors/`，并自动注册到主程序。

3. **编译和运行 windows_exporter**

   - **依赖安装**
     - 确保已安装 Go 环境（建议 1.18 及以上）。
     - 进入 `windows_exporter` 目录，执行：
       ```bash
       go mod tidy
       ```

   - **编译命令**
     - 在 `windows_exporter` 目录下执行：
       ```bash
       go build -o windows_exporter.exe ./cmd/windows_exporter
       ```
     - 生成的可执行文件为 `windows_exporter.exe`（Windows 下）或 `windows_exporter`（Linux 下）。

   - **静态编译说明**
     - 如需生成纯静态可执行文件（无需依赖 C 运行库），可使用如下命令：
       ```bash
       CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o windows_exporter.exe ./cmd/windows_exporter
       ```
     - 推荐优先使用 `CGO_ENABLED=0`，这样生成的二进制文件更易于分发和跨平台运行。
     - 极个别硬件信息如采集不到，可尝试 `CGO_ENABLED=1` 重新编译。
     - 依赖的 `ghw` 和 `gopsutil` 库大部分功能为纯 Go 实现，静态编译通常可用，但极少数底层硬件信息在部分环境下可能因无 cgo 支持而采集不到，表现为“未知”或0。

   - **运行方式**
     - 直接运行：
       ```bash
       ./windows_exporter.exe
       ```
     - 常用参数示例：
       - `--web.listen-address=:9182` 监听端口（默认 9182）
       - `--collectors.enabled="hardware_info,..."` 仅启用部分采集器
       - `--log.level=debug` 输出调试日志

   - **验证采集器生效**
     - 启动后访问：
       ```
       http://localhost:9182/metrics
       ```
     - 搜索 `node_hardware_` 前缀的指标，确认采集器已生效。

4. **自定义缓存刷新周期**
   - 可通过环境变量 `HARDWARE_INFO_INTERVAL` 设置硬件信息缓存刷新周期（如 `2h`、`30m`），默认8小时。
   - Windows 下设置示例：
     ```cmd
     set HARDWARE_INFO_INTERVAL=2h
     ```
   - Linux/macOS 下设置示例：
     ```bash
     export HARDWARE_INFO_INTERVAL=2h
     ```

## 内存与性能说明

- 采集器只在刷新周期到达时才采集一次硬件信息，其余时间均直接读取缓存，极大提升性能。
- 缓存中始终只保存一份最新的 `HardwareInfo`，不会无限增长，无内存泄漏风险。
- 采集失败时有“未知”兜底，避免 panic。
- 支持多线程并发 scrape，线程安全。

## 常见问题

- **指标未出现/为0/为未知？**
  - 检查依赖库是否安装完整。
  - 检查采集器是否已被自动注册（查看主注册文件 import 和 init 部分）。
  - 某些硬件信息在虚拟机或部分硬件环境下可能无法获取，属正常现象。
  - 静态编译（`CGO_ENABLED=0`）下极少数底层硬件信息可能采集不到，可尝试 `CGO_ENABLED=1` 重新编译。

- **如何扩展采集内容？**
  - 可在 `HardwareInfo` 结构体和 `collectHardwareInfo` 函数中添加自定义字段和采集逻辑。

## 参考

- [ghw 官方文档](https://github.com/jaypipes/ghw)
- [gopsutil 官方文档](https://github.com/shirou/gopsutil)
- [Prometheus 官方文档](https://prometheus.io/docs/instrumenting/writing_exporters/)

## Windows 可执行文件图标说明

在 Windows 下，若需让编译生成的 `windows_exporter.exe` 带有自定义图标，需要在编译前生成 `.syso` 资源文件。

### 步骤一：生成 .syso 文件

1. 确保 `windows_exporter/cmd/windows_exporter/winres/` 目录下有 `icon.png` 和 `winres.json`。
2. 安装 go-winres 工具（如未安装）：
   ```bash
   go install github.com/tc-hib/go-winres@latest
   ```
3. 进入主程序目录并生成 .syso 文件：
   ```bash
   cd windows_exporter/cmd/windows_exporter
   go-winres make
   ```
   这会生成 `winres_windows_amd64.syso` 文件。

### 步骤二：编译时确保 .syso 文件在 main.go 同目录

- 在项目根目录下运行：
  ```bash
  CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o windows_exporter.exe ./cmd/windows_exporter
  ```
- 编译器会自动将同目录下的 `.syso` 文件嵌入到最终的 `.exe` 文件中。

### 注意事项

- 若未生成 `.syso` 文件或文件位置不对，编译出的 `.exe` 将不会有自定义图标。
- 如需更换图标，只需替换 `icon.png` 并重新执行上述步骤。
- 该流程与是否启用 CGO 无关。

---

如有更多问题或定制需求，请联系维护者或提交 issue。 
