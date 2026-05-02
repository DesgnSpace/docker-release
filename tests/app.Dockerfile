FROM nginx:alpine
ARG BUILD_ID
RUN echo "build: ${BUILD_ID}" > /usr/share/nginx/html/version.txt
COPY nginx/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
HEALTHCHECK --interval=5s --timeout=3s --retries=3 --start-period=10s CMD wget -q -O /dev/null http://127.0.0.1/healthz || exit 1
ENTRYPOINT ["/entrypoint.sh"]
