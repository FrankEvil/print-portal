# AirPrint / Mopria 无线打印

## 目标

让 iPhone、iPad、Mac 通过 AirPrint 自动发现 Print Portal 的共享打印机；Android 通过 Default Print Service / Mopria 发现同一个 IPP 服务。

不依赖宿主 Avahi、D-Bus 或额外的 cups-airprint 容器。

## 实现方式

Print Portal 在 `printd` 内启动一个纯 Go 的 mDNS/DNS-SD responder，直接发布：

```text
_ipp._tcp
_universal._sub._ipp._tcp
```

每台开启 AirPrint 的共享打印机发布一个服务：

```text
实例名：AirPrint <打印机名> @ <主机名>
端口：8631
```

TXT 记录：

```text
txtvers=1
qtotal=1
rp=printers/<ppd_queue>
pdl=application/pdf,image/urf,image/jpeg,image/png
URF=none
product=(Print Portal)
note=<打印机名>
printer-state=3
```

只广播 PPD 队列，不广播 RAW 队列。

## 配置

在共享页展开对应打印机，开启：

```text
允许访问
AirPrint
```

AirPrint 是每台打印机独立开关。只有同时满足：

```text
is_shared=true
airprint_enabled=true
```

才会广播该打印机的 PPD 队列。

## 网络要求

必须使用 host network：

```yaml
network_mode: host
```

并且局域网必须允许：

```text
UDP 5353 组播
IGMP / mDNS
```

如果使用 VLAN，需要路由器开启 mDNS/Bonjour 反射。

## 本机验证

在 Mac 上使用：

```bash
dns-sd -B _ipp._tcp local
```

可以看到：

```text
AirPrint Brother DCP-7060D @ fnos
```

解析服务：

```bash
dns-sd -L "AirPrint Brother DCP-7060D @ fnos" _ipp._tcp local
```

返回：

```text
fnos.local.:8631
rp=printers/brother-dcp-7060d-ppd
pdl=application/pdf,image/urf,image/jpeg,image/png
```

IPP 属性查询：

```bash
ipptool -tv ipp://192.168.2.30:8631/printers/brother-dcp-7060d-ppd get-printer-attributes.test
```

返回：

```text
status-code = successful-ok
printer-is-accepting-jobs = true
printer-state = idle
```

实际提交测试页：

```bash
ipptool -tv -f /tmp/pp-test.pdf \
  ipp://192.168.2.30:8631/printers/brother-dcp-7060d-ppd print-job.test
```

返回：

```text
Print file using Print-Job [PASS]
status-code = successful-ok
job-id = 3
job-state = pending
```

CUPS 历史记录：

```text
brother-dcp-7060d-ppd-3  11264 bytes  completed
```

## 网络风险

fnOS 的 `enp6s18-ovs` 网卡上用 `dig @224.0.0.251` 查询可能出现 timeout，但 Mac 的 `dns-sd` 实际可以发现并解析，说明不能只用宿主机 `dig` 判断 AirPrint 是否可用。

如果手机发现不稳定，检查：

- Open vSwitch / 网桥组播设置
- 路由器 IGMP snooping
- Wi-Fi 客户端隔离
- VLAN 之间的 mDNS 反射
- 手机和 NAS 是否在同一广播域

## Android 连接

Android 8+：

```text
设置 → 连接设备 → 打印 → 默认打印服务
```

或者安装：

```text
Mopria Print Service
```

如果局域网 mDNS 正常，应该能看到 `AirPrint <打印机名>` 或对应 IPP 服务。

无论 mDNS 是否可用，Android 也可以直接使用 Print Portal 网页：

```text
浏览器打开 Print Portal → 上传文件 → 打印
```

## 任务记录

AirPrint / ipptool 直接提交到 CUPS 的任务不会经过网页上传流程，但 `printd` 会在启动和状态同步时导入最近 10 分钟完成的 CUPS 任务，任务来源显示为「本地」，文件名为「AirPrint / IPP 打印」。

## 不建议

- 不广播 RAW 队列
- 不使用 `cups-airprint` 独立容器
- 不依赖宿主 D-Bus
- 不在每次状态变化时重建服务名
