VERSION ?= $(shell v=$$(git tag --points-at HEAD 2>/dev/null | head -1); echo $${v:-dev})
IMAGE   ?= malico/docker-release

.PHONY: dev test build publish buildx-builder up-nginx up-angie up-traefik up-nginx-proxy down-nginx down-angie down-traefik down-nginx-proxy

build: buildx-builder
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		-t $(IMAGE):$(VERSION) \
		-t $(IMAGE):latest \
		.

publish: buildx-builder
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		-t $(IMAGE):$(VERSION) \
		-t $(IMAGE):latest \
		--push \
		.

buildx-builder:
	@docker buildx inspect docker-release-builder >/dev/null 2>&1 || \
		docker buildx create --name docker-release-builder --driver docker-container --use

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
