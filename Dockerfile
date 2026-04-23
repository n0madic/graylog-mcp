FROM --platform=$BUILDPLATFORM golang:alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
	go build -trimpath -ldflags="-s -w" -o /out/graylog-mcp .

FROM gcr.io/distroless/static-debian12:nonroot

ENV GRAYLOG_MCP_TRANSPORT=http \
	GRAYLOG_MCP_HTTP_BIND=0.0.0.0:8090

COPY --from=builder /out/graylog-mcp /bin/graylog-mcp

EXPOSE 8090

ENTRYPOINT ["graylog-mcp"]
