# Built by ship-agent on the server from a release binary that matched the
# release's SHA256SUMS. BINARY is the downloaded asset.
FROM alpine:3.22

ARG VERSION=dev
ARG BINARY=hello

RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -g 10001 -S hello \
 && adduser -u 10001 -S -D -G hello -h /data hello

COPY ${BINARY} /usr/local/bin/hello

ENV HELLO_LISTEN=:8080
EXPOSE 8080
USER 10001:10001
ENTRYPOINT ["hello"]
CMD ["serve"]
