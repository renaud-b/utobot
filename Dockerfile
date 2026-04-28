FROM golang:1.26.2-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/utobot .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S utobot \
    && adduser -S -G utobot utobot

WORKDIR /opt/utobot

COPY --from=build /out/utobot /usr/local/bin/utobot

USER utobot

EXPOSE 9000

ENTRYPOINT ["/usr/local/bin/utobot"]
CMD ["/opt/utobot/config.json"]
