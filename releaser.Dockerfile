FROM alpine:3.17

WORKDIR /
COPY manager /manager
ENTRYPOINT ["/manager"]
