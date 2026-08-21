FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/readproofd ./cmd/readproofd

FROM alpine:3.24
RUN apk add --no-cache ca-certificates

# readproofd needs nothing root can do: the binary is static (CGO_ENABLED=0),
# it binds 8080 rather than a privileged port, and it writes only under its
# data directory. Running it as root would mean a bug in a source adapter runs
# as root too.
RUN addgroup -S -g 65532 readproof \
 && adduser -S -u 65532 -G readproof -H -D readproof

# The embedded backend's SQLite database and blob store live here, and this is
# the path to mount a volume on. It is created and chowned in the image
# because Docker copies the mount point's ownership from the image when it
# initializes a named volume — do it here and the volume comes up writable by
# the non-root user; do it later and it does not.
RUN mkdir -p /var/lib/readproof && chown readproof:readproof /var/lib/readproof

COPY --from=build /out/readproofd /usr/local/bin/readproofd

USER readproof:readproof
WORKDIR /var/lib/readproof
# Ignored in Postgres mode (READPROOFD_POSTGRES_DSN); in embedded mode it puts
# the data where the volume is, rather than in whatever the working directory
# happens to be.
ENV READPROOFD_DATA_DIR=/var/lib/readproof
EXPOSE 8080
ENTRYPOINT ["readproofd"]
