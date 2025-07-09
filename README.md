# windows_exporter 插件化采集器

windows_exporter 是一个面向 Windows 系统的高性能 Prometheus 监控采集器，支持插件化扩展，方便用户根据实际需求开发和集成自定义采集器（插件）。

## 主要特性
- **插件化机制**：支持通过 my_collectors 目录和自动注册脚本，灵活扩展自定义采集器。
- **多平台支持**：支持 Windows/amd64 和 Windows/arm64 架构。
- **自动化构建与发布**：集成 GitHub Actions 自动编译、发布、清理。
- **丰富的内置采集器**：覆盖常见 Windows 性能、服务、硬件、网络等监控需求。

## 插件（自定义采集器）机制

- 所有自定义采集器（插件）建议放在 `my_collectors/` 目录下。
- 通过 `my_collectors/auto_register_collectors.sh` 脚本自动注册插件，无需手动修改主程序代码。
- 插件开发文档和示例可参考 `my_collectors/hardware_info_collector.md`。

### 插件开发与集成流程
1. 在 `my_collectors/` 目录下新建你的采集器 Go 文件（如 `my_plugin_collector.go`）。
2. 按照内置采集器的接口规范实现采集逻辑。
3. 运行 `my_collectors/auto_register_collectors.sh` 自动注册插件。
4. 重新编译 windows_exporter，插件会自动集成到主程序。

## 目录结构说明

```
windows_exporter/
├── my_collectors/           # 插件（自定义采集器）目录
│   ├── auto_register_collectors.sh  # 插件自动注册脚本
│   ├── hardware_info_collector.go   # 插件示例
│   └── ...
├── windows_exporter/        # 主程序源码
│   ├── internal/collector/  # 内置采集器
│   └── ...
├── .github/workflows/       # 自动化 CI/CD 配置
├── README.md                # 项目说明文档
└── ...
```

## 快速开始

1. **拉取代码**
   ```sh
   git clone https://github.com/your-org/windows_exporter.git
   cd windows_exporter
   ```
2. **开发自定义插件**
   - 在 `my_collectors/` 目录下新建 Go 文件，实现采集逻辑。
   - 运行 `bash my_collectors/auto_register_collectors.sh` 自动注册。
3. **编译主程序**
   ```sh
   cd windows_exporter
   go mod tidy
   go build -o windows_exporter.exe ./cmd/windows_exporter
   ```
4. **运行**
   ```sh
   ./windows_exporter.exe
   ```

## 贡献
欢迎提交自定义采集器插件或改进建议！

---

如需详细插件开发文档、接口说明或遇到问题，请查阅 `my_collectors/` 下的文档或在 Issues 区提问。