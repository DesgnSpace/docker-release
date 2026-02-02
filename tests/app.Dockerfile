FROM nginx:alpine
ARG BUILD_ID
RUN echo "build: ${BUILD_ID}" > /usr/share/nginx/html/version.txt
COPY nginx/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
