# Brother DCP-7060D Linux 驱动调查

**来源**：[Brother 中国支持下载页](https://support.brother.com/g/b/downloadlist.aspx?c=cn&lang=zh&prod=dcp7060d_all&os=128&flang=English)（Linux deb 分类）

## 下载项

- Driver Install Tool（安装 LPR、CUPSwrapper，扫描型号还含 scanner 驱动）
- LPR printer driver (deb package) 2.1.0-1
- CUPSwrapper printer driver (deb package) 2.0.4-2
- Generic LPR / Generic CUPSwrapper（deb）
- Scanner driver 32bit / 64bit（deb）
- Scan-key-tool 32bit / 64bit（deb）

## 包内容检查

两个打印驱动包均为 **i386（32 位）**：

```text
dcp7060dlpr-2.1.0-1.i386.deb
cupswrapperDCP7060D-2.0.4-2.i386.deb
```

包含：

```text
/usr/local/Brother/Printer/DCP7060D/
├── cupswrapper/brcupsconfig4
├── inf/brprintconflsr3
├── inf/brDCP7060Dfunc
├── inf/brDCP7060Drc
├── lpd/filterDCP7060D
├── lpd/psconvert2
└── lpd/rawtobr3
```

- `filterDCP7060D` 是打印过滤脚本，调用 `psconvert2` / `rawtobr3`
- `brprintconflsr3` 用于打印参数配置（分辨率、纸张、双面、Toner Save 等）
- **没有发现**清洗、校准、固件、耗材状态等维护 CLI
- macOS 驱动中的 `BrStatusMonitor.app` 是 GUI，不能直接运行在 Linux 容器

## 结论

- 可以用该 Linux 驱动在服务器上做 PPD/原生打印（需容器支持 i386 运行库）
- 但该驱动没有维护 CLI，Web 端只能提供通用 CUPS 状态、PPD 选项、测试页
- DCP-7060D 的完整维护/状态 GUI 仍需要本地 USB 连接 Mac/Windows，或 USB/IP 转发
