FROM docker.io/khulnasoft/test-infra/docker-dind

ARG TARGETARCH

ENV PUBLISH_S3="false"

RUN wget -q https://github.com/khulnasoft/dbg-go/releases/download/v0.16.0/dbg-go_0.16.0_linux_$TARGETARCH.tar.gz \
    && tar -xvf dbg-go_0.16.0_linux_$TARGETARCH.tar.gz \
    && chmod +x dbg-go \
    && mv dbg-go /bin/dbg-go

COPY build-drivers.sh /workspace/build-drivers.sh

RUN chmod 755 /workspace/build-drivers.sh

ENV DOCKER_API_VERSION=1.41

WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["bash"]
