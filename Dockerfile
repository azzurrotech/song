#
# song — static hosting with server-side template processing and Web Components.
# Standard library only: no external Go modules are fetched at build time.
#
# The container listens on port 8083 by default (--port / SONG_PORT).
#

FROM golang:1.20-alpine AS build

WORKDIR /src

# No go.sum / external requires: all dependencies are the Go standard library
# and packages inside this module, so only the source tree is needed.
COPY go.mod ./
COPY pkg/ pkg/
COPY main.go ./

RUN go build -trimpath -ldflags="-s -w" -o /out/song-server .

FROM alpine:3.19

RUN adduser -D -H -u 1000 song
WORKDIR /srv

COPY --from=build /out/song-server /usr/local/bin/song-server

RUN mkdir -p /data && chown song:song /data

USER song

# Per-silo store root: static files, .templates/ and .song/meta.json.
VOLUME ["/data"]
EXPOSE 8083

ENTRYPOINT ["song-server"]
CMD ["--port=8083", "--root=/data"]