
# 背景

数据库场景下，实例需要固定ip，在删除pod后，重新创建的pod仍然需要使用源ip

cilium已经支持了ipam扩展，但有所局限

1. 安装时，cilium中配置ipam指定为crd： `ipam: crd`，并且不要配置`cluster-pool`
2. 当配置为crd时，所有ip都需要由外部组件来分配，cilium不会进行任何的分配。每个主机的网段，将使用k8s源生的controller-manager的cidr分配策略
3. ip分配方式

ciliumNodes资源配置：
```yaml
spec:
    addresses:
    - ip: 10.10.40.107
      type: InternalIP
    ipam:
      podCIDRs:
      - 10.244.1.0/24
      pool:
        10.244.1.1: {}
        10.244.1.2: {}
        10.244.1.3: {}
        10.244.1.4: {}
        10.244.1.5: {}
        10.244.1.6: {}

```
ipam.podIIDRs，由k8s分配，cilium会自动同步过来

pool中的ip列表，需要外部组件来指定。

有两个特殊的ip使用规则：route与health，是cilium使用的，也需要分配：

```yaml
10.244.1.1:
  owner: router
10.244.1.2:
  owner: health
```

pool中，可以指定owner，除了特殊的`router`与`health`外，其它owner命名规则为：`<pod_namespace>/<pod_name>`，比如：`kube-system/coredns-546565776c-6vhnn`

# ip分配策略

## 1. 启动时，先分配两个特殊的ip，用于cilium的router与health

## 2. 普通pod的分配

启动时，直接分配10个可用ip（按ip顺序），当发现可用ip不足5个时，再增加10个ip

之后cilium发现有可用ip时，会自动进行ip分配

## 3. 特殊pod的ip分配

通过webhook方式，监听特殊pod的更新（基于标签），当pod完成调度时，（nodeName从无到有），将按如下处理：

1. 检查pod是否已经分配过，如果已分配，则不处理
2. 从未分配的ip中找一个，并指定owner，并记录resource为sts的namespace/name，并更新ciliumNode资源成功。

## 4. 特殊pod的ip回收

1. 定期检查已指定owner的ip，如果sts已删除，则释放对应的ip信息（将owner与resource清空）

