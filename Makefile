VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
IMAGE   ?= malico/docker-release

.PHONY: dev test build publish up-nginx up-angie up-traefik up-nginx-proxy down-nginx down-angie down-traefik down-nginx-proxy

build:
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		-t $(IMAGE):$(VERSION) \
		-t $(IMAGE):latest \
		.

publish:
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		-t $(IMAGE):$(VERSION) \
		-t $(IMAGE):latest \
		--push \
		.

# Install the Docker CLI plugin (docker release <cmd>)
dev:
	mkdir -p ~/.docker/cli-plugins
	ln -sf $(PWD)/scripts/docker-release ~/.docker/cli-plugins/docker-release
	chmod +x scripts/docker-release
	@echo "Done — run: docker release <service>"

test:
	go test ./...

up-nginx:
	docker compose -f tests/nginx/docker-compose.yml up --build

up-angie:
	docker compose -f tests/angie/docker-compose.yml up --build

up-traefik:
	docker compose -f tests/traefik/docker-compose.yml up --build

up-nginx-proxy:
	docker compose -f tests/nginx-proxy/docker-compose.yml up --build

down-nginx:
	docker compose -f tests/nginx/docker-compose.yml down -v

down-angie:
	docker compose -f tests/angie/docker-compose.yml down -v

down-traefik:
	docker compose -f tests/traefik/docker-compose.yml down -v

down-nginx-proxy:
	docker compose -f tests/nginx-proxy/docker-compose.yml down -v
