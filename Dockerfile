#
# song — static hosting, server-side rendering, encrypted files.
# Standard library only: no external Go modules are fetched at build time.
#
# Host port for the song service is 8083 (see AGENTS.md port table).
#

FROM golang:1.20-alpine AS build

WORKDIR /src

# No go.sum / external requires: all dependencies are the Go standard library
# and packages inside this module, so only the source tree is needed.
COPY go.mod ./
COPY internal/ internal/
COPY pkg/ pkg/
COPY main.go ./

RUN go build -trimpath -ldflags="-s -w" -o /out/song-server .

FROM alpine:3.19

RUN adduser -D -H -u 1000 song
WORKDIR /srv

COPY --from=build /out/song-server /usr/local/bin/song-server

RUN mkdir -p /data && chown song:song /data

USER song

# Per-silo store root: files, .templates, .song/meta.json, encrypted content.
VOLUME ["/data"]
EXPOSE 8083

ENTRYPOINT ["song-server"]
CMD ["--port=8083", "--root=/data"]