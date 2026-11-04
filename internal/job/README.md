# internal/job

打印任务状态机：

```text
pending -> converting -> submitted -> queued -> printing -> completed
                                        \-> failed
                                        \-> cancelled
```

任务持久化到 SQLite，重启不丢。
