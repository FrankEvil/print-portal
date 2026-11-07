# internal/discovery

设备和扫描仪发现：

- USB：`lpinfo -v`
- IPP/AirPrint：内置 Go mDNS browser（`_ipp._tcp` / `_ipps._tcp`）
- 扫描仪：`scanimage -L` / `airscan-discover`
- 自动匹配 PPD 驱动
