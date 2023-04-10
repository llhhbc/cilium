
set -x
set -e

for image in cilium:v1.12.7 \
    operator-generic:v1.12.7 \
    hubble-relay:v1.12.7 \
    hubble-ui:v0.9.2 \
    hubble-ui-backend:v0.9.2;do \

    skopeo copy --insecure-policy --override-os linux \
    --override-arch arm64 \
    docker://quay.io/cilium/${image} \
    docker://registry.woqutech.com/woqutech/cilium-${image}-arm

    skopeo copy --insecure-policy --override-os linux \
    --override-arch amd64 \
    docker://quay.io/cilium/${image} \
    docker://registry.woqutech.com/woqutech/cilium-${image}
done

