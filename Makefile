.PHONY: dev dev-standalone test

# Install the Docker CLI plugin (docker release <cmd>)
dev:
	mkdir -p ~/.docker/cli-plugins
	ln -sf $(PWD)/scripts/docker-release ~/.docker/cli-plugins/docker-release
	chmod +x scripts/docker-release
	@echo "Done — run: docker release <service>"

# Install as a standalone script (dr <cmd>)
dev-standalone:
	ln -sf $(PWD)/scripts/dr /usr/local/bin/dr
	chmod +x scripts/dr
	@echo "Done — run: dr <service>"

test:
	go test ./...
