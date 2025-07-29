FROM alpine:3.22

WORKDIR /
COPY manager /manager
ENTRYPOINT ["/manager"]
