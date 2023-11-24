
docker buildx build --build-arg HTTPS_PROXY=http://10.10.80.176:7890 -t registry.woqutech.com/longhui.li/cilium-runtime:myv1.12.7 . --push

