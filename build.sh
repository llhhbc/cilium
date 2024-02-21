
set -ex

revision=`git rev-parse HEAD`

docker buildx build --label commit=$revision -t registry.woqutech.com/woqutech/cilium-cilium:v1.12.7-hcs-oem -f ./images/cilium/Dockerfile . --push

docker pull registry.woqutech.com/woqutech/cilium-cilium:v1.12.7-hcs-oem
docker tag registry.woqutech.com/woqutech/cilium-cilium:v1.12.7-hcs-oem registry.woqutech.com/woqutech/cilium-cilium:v1.12.7-ipam-vpc-hcs-oem

docker push registry.woqutech.com/woqutech/cilium-cilium:v1.12.7-ipam-vpc-hcs-oem

