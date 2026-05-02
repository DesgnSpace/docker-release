FROM traefik/whoami
ARG BUILD_ID
ENV WHOAMI_NAME="${BUILD_ID}"
HEALTHCHECK --interval=5s --timeout=3s --retries=3 --start-period=5s CMD ["/whoami", "--check"]
