FROM --platform=$TARGETPLATFORM registry.woqutech.com/google_containers/alpine:3.13 as prod
COPY ./cepKeeper /cepKeeper
ENTRYPOINT ["/cepKeeper"]
