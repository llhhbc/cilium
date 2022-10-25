FROM --platform=$TARGETPLATFORM registry.woqutech.com/google_containers/alpine:3.13 as prod
COPY ./ipam /ipam
EXPOSE 443
ENTRYPOINT ["/ipam"]
