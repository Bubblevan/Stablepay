FROM alpine:3.21

RUN addgroup -S -g 10001 runtime \
    && adduser -S -D -H -u 10001 -G runtime runtime

WORKDIR /app
COPY e4-local-runtime /app/e4-local-runtime
USER 10001:10001
EXPOSE 8090
ENTRYPOINT ["/app/e4-local-runtime"]
