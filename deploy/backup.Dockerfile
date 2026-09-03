FROM alpine:3.21

RUN apk add --no-cache \
        ca-certificates \
        postgresql16-client \
        rclone \
        tzdata

COPY backup.sh /usr/local/bin/backup.sh
RUN chmod 0755 /usr/local/bin/backup.sh

ENTRYPOINT ["/usr/local/bin/backup.sh"]
CMD ["--loop"]
