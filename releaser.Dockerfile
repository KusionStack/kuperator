
# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM alpine:3.17
WORKDIR /

COPY manager /manager
RUN mkdir /webhook-certs && chmod 777 /webhook-certs
ENTRYPOINT ["/manager"]
