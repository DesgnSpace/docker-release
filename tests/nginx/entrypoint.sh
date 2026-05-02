#!/bin/sh
BUILD=$(cat /usr/share/nginx/html/version.txt)
RANDOM_ID=$(head -c 8 /dev/urandom | base64 | tr -dc a-z0-9 | head -c 6)
TIMESTAMP=$(date '+%Y-%m-%d %H:%M:%S')
cat > /usr/share/nginx/html/health <<EOF
ok
EOF

cat > /usr/share/nginx/html/index.html <<EOF
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Deployment Strategies Test</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      min-height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
      background: #f5f5f5;
    }
    .container {
      background: white;
      padding: 2.5rem;
      border-radius: 12px;
      box-shadow: 0 4px 20px rgba(0,0,0,0.08);
      max-width: 400px;
      width: 90%;
    }
    h1 {
      font-size: 1.5rem;
      color: #1a1a1a;
      margin-bottom: 0.5rem;
    }
    .info {
      color: #666;
      font-size: 0.9rem;
      margin-bottom: 2rem;
      padding-bottom: 1.5rem;
      border-bottom: 1px solid #eee;
    }
    .info span {
      display: block;
      margin-top: 0.5rem;
    }
    .label {
      font-weight: 600;
      color: #333;
    }
    .session {
      font-family: monospace;
      font-size: 0.8rem;
      background: #f0f0f0;
      padding: 0.25rem 0.5rem;
      border-radius: 4px;
      color: #666;
    }
    h2 {
      font-size: 1rem;
      color: #888;
      text-transform: uppercase;
      letter-spacing: 0.05em;
      margin-bottom: 1rem;
    }
    .links {
      display: flex;
      flex-direction: column;
      gap: 0.75rem;
    }
    .links a {
      display: block;
      padding: 1rem;
      background: #f8f9fa;
      color: #2563eb;
      text-decoration: none;
      border-radius: 8px;
      font-weight: 500;
      transition: all 0.2s;
      text-align: center;
    }
    .links a:hover {
      background: #2563eb;
      color: white;
      transform: translateY(-2px);
    }
  </style>
</head>
<body>
  <div class="container">
    <h1>Deployment Strategies</h1>
    <div class="info">
      <span><span class="label">Container:</span> $(hostname)</span>
      <span><span class="label">Build:</span> ${BUILD}</span>
      <span><span class="label">Session:</span> <span class="session">${RANDOM_ID}</span></span>
      <span><span class="label">Time:</span> ${TIMESTAMP}</span>
    </div>
    <h2>Select Strategy</h2>
    <div class="links">
      <a href="/linear/">Linear Deployment</a>
      <a href="/canary/">Canary Deployment</a>
      <a href="/bluegreen/">Blue-Green Deployment</a>
    </div>
  </div>
</body>
</html>
EOF
exec nginx -g 'daemon off;'
