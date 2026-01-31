#!/bin/sh
BUILD=$(cat /usr/share/nginx/html/version.txt)
cat > /usr/share/nginx/html/index.html <<EOF
container: $(hostname)
${BUILD}
EOF
exec nginx -g 'daemon off;'
