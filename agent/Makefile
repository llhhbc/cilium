
build:
	GOOS=linux go build ../daemon
	docker build -t registry.cn-hangzhou.aliyuncs.com/tomc/qfusion/mycilium:v1.9.5 .
	docker push registry.cn-hangzhou.aliyuncs.com/tomc/qfusion/mycilium:v1.9.5
