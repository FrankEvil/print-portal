# internal/cups

CUPS 适配层：

- `lp`, `lpstat`, `cancel`, `lpadmin`, `cupsctl` 命令封装
- IPP 查询/打印（后续）
- 打印队列、PPD 选项、任务状态

对外接口应可替换成 fake，方便测试。
