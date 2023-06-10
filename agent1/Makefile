
build:
	export DOCKER_HOST="tcp://10.10.40.23:2375"
	GOOS=linux go build ../daemon
	docker buildx build -t registry.woqutech.com/longhui.li/mycilium:mytest --push .
# 	docker build -t registry.woqutech.com/longhui.li/mycilium:ipam-vpc2 .
# 	docker push registry.woqutech.com/longhui.li/mycilium:ipam-vpc2

build-arm:
	export DOCKER_HOST="tcp://10.10.40.23:2375"
	GOOS=linux GOARCH=arm64 go build ../daemon
	docker buildx use mybuild
	docker buildx build --platform=linux/arm64 --push -t registry.woqutech.com/longhui.li/mycilium:ipam-vpc-arm .
# 	skopeo copy --insecure-policy --override-os linux --override-arch arm64 --dest-tls-verify=false docker://registry.woqutech.com/longhui.li/mycilium:ipam-vpc-arm docker://124.70.148.62:5000/woqutech/mycilium:ipam-vpc-arm

build-cli:
	GOOS=linux go build ../cilium
	docker buildx build -t registry.woqutech.com/longhui.li/mycilium:mytest-cli -f Dockerfile_cli --push .
