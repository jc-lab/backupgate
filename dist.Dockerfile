FROM alpine:3.23

RUN apk --no-cache add \
    ca-certificates shadow

WORKDIR /app

RUN groupadd -g 1001 backupgate && \
    useradd -g 1001 -u 1001 -d /app backupgate

ARG TARGETPLATFORM

COPY dist/${TARGETPLATFORM}/backupgate.exe /app/backupgate.exe

RUN chmod +x /app/backupgate.exe

USER backupgate

CMD ["/app/backupgate.exe", "--config", "/etc/backupgate/config.yaml"]
