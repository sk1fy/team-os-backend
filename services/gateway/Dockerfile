# syntax=docker/dockerfile:1.7
FROM golang:1.26.10-alpine AS build
WORKDIR /src

COPY go.work go.work.sum* ./
COPY pkg/go.mod pkg/go.sum ./pkg/
COPY contracts/gen/go/go.mod contracts/gen/go/go.sum* ./contracts/gen/go/
COPY services/gateway/go.mod services/gateway/go.sum ./services/gateway/
RUN cd services/gateway && GOWORK=off go mod download

COPY pkg ./pkg
COPY contracts/gen/go ./contracts/gen/go
COPY services/gateway ./services/gateway
ARG BUILD_REVISION=unknown
RUN cd services/gateway && GOWORK=off CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/sk1fy/team-os-backend/pkg/buildinfo.Revision=${BUILD_REVISION}" -o /out/gateway ./cmd/gateway

FROM gcr.io/distroless/static-debian12:nonroot
ARG BUILD_REVISION=unknown
LABEL org.opencontainers.image.revision=$BUILD_REVISION
WORKDIR /app
COPY --from=build /out/gateway /app/gateway
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/gateway"]
