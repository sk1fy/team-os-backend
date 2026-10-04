# CI fixture of the same release used by deploy/docker-compose.yaml.
# Upstream tag RELEASE.2025-04-22T22-12-26Z resolves to this exact commit.
FROM golang:1.25.14-alpine@sha256:1ae0735f00daffa3aaf1363a5184c0d2dc55c78e3db4ec70241cdac97bf84b59 AS build
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOMAXPROCS=2
WORKDIR /src
RUN wget -O /tmp/minio.tar.gz https://codeload.github.com/minio/minio/tar.gz/0d7408fc9969caf07de6a8c3a84f9fbb10a6739e \
    && echo '7eb30a913fea30f18069abf194e1e78e4983b558cc526911ae1c11396a9859a5  /tmp/minio.tar.gz' | sha256sum -c - \
    && tar -xzf /tmp/minio.tar.gz --strip-components=1 -C /src && rm /tmp/minio.tar.gz
RUN go build -mod=readonly -p=2 -tags kqueue -trimpath -buildvcs=false \
      -ldflags="-s -w -X github.com/minio/minio/cmd.Version=2025-04-22T22:12:26Z -X github.com/minio/minio/cmd.CopyrightYear=2025 -X github.com/minio/minio/cmd.ReleaseTag=RELEASE.2025-04-22T22-12-26Z -X github.com/minio/minio/cmd.CommitID=0d7408fc9969caf07de6a8c3a84f9fbb10a6739e -X github.com/minio/minio/cmd.ShortCommitID=0d7408fc9969" \
      -o /out/minio .

FROM alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8
RUN apk add --no-cache ca-certificates curl
COPY --from=build /out/minio /usr/local/bin/minio
COPY --from=build /src/dockerscripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
COPY --from=build /src/LICENSE /usr/share/licenses/minio/LICENSE
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
EXPOSE 9000 9001
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["minio"]
