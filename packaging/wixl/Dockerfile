# Disposable Ubuntu 26.04 MSI toolchain for DigiLogic Linux packaging probes.
# Neither product's current Windows installer is replaced by this image.
FROM ubuntu:26.04
ENV DEBIAN_FRONTEND=noninteractive
RUN set -eux; \
    if test -f /etc/apt/sources.list.d/ubuntu.sources; then \
      sed -i 's/^Components: main$/Components: main universe/' /etc/apt/sources.list.d/ubuntu.sources; \
    fi; \
    apt-get update; \
    apt-get install -y --no-install-recommends wixl msitools ca-certificates; \
    rm -rf /var/lib/apt/lists/*
ENV HOME=/tmp
WORKDIR /probe
