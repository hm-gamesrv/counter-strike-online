FROM golang:1.27-bookworm AS build
# 国内网络可 --build-arg GOPROXY=https://goproxy.cn,direct
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY assets ./assets
COPY internal ./internal
COPY cmd ./cmd
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /cso-server ./cmd/cso-server \
    && mkdir /data && chown 1000:1000 /data

FROM scratch
COPY --from=build /cso-server /app/cso-server
COPY --from=build --chown=1000:1000 /data /data
USER 1000:1000
WORKDIR /data
VOLUME ["/data"]
EXPOSE 30002/tcp 30002/udp
ENTRYPOINT ["/app/cso-server", "-data", "/data"]
