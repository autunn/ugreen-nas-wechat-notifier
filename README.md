NasNotify Go 项目说明文档
一、核心功能
实时监控：自动获取 NAS CPU 使用率、温度、风扇转速、内存占用及网络流量。
状态概览：支持查询存储卷容量、Docker 容器运行状态、进程 CPU/内存占用。
交互式控制：支持风扇控制、CPU 性能管理（高性能/均衡/节能模式）。
自动推送：定时自动抓取系统通知并推送至企业微信。
二、快速部署
本项目支持 Docker 一键部署：
docker run -d --name nasnotify-go -v ./config:/app/config -v ./data:/app/data ghcr.io/autunn/nasnotify-go:latest
三、功能菜单与指令
菜单/指令	功能描述
监控-系统状态	获取 CPU、内存、风扇及网络实时数据
监控-存储状态	获取所有存储卷的容量及使用率
服务-Docker	查看 Docker 容器运行状态及镜像信息
服务-进程列表	获取当前 CPU 占用最高的前 5 个进程
指令-风扇 X	1=静音, 2=正常, 3=全速
指令-CPU X	0=高性能, 1=均衡, 2=节能
