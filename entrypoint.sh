#!/bin/sh

# If the nginx template shared dir is mounted, seed it with the default
# template so nginx-proxy can start immediately.
NGINX_TMPL_DIR="/shared/nginx-tmpl"
if [ -d "$NGINX_TMPL_DIR" ] && [ ! -f "$NGINX_TMPL_DIR/nginx.tmpl" ]; then
    cp /builds/nginx.tmpl "$NGINX_TMPL_DIR/nginx.tmpl"
fi

exec docker-release "$@"
