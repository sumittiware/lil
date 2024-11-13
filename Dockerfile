FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates=* \
        tzdata=* && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* && \
    rm -rf /var/cache/apt/*

WORKDIR /app
COPY lil.bin /app/lil
COPY config.sample.toml /app/config.toml

EXPOSE 7000

CMD ["/app/lil", "--config=/app/config.toml"]
