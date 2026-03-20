IMAGE       := ghcr.io/cryptolabinc/rune-vault
TAG         ?= latest
PLATFORMS   := linux/amd64,linux/arm64
BUILDER     := rune-vault-builder

.PHONY: build push login builder clean

builder:
	@docker buildx inspect $(BUILDER) >/dev/null 2>&1 || \
		docker buildx create --name $(BUILDER) --use --driver docker-container

login:
	@gh auth token | docker login ghcr.io -u $(shell gh api user -q .login) --password-stdin

build: builder
	docker buildx build --builder $(BUILDER) \
		-t $(IMAGE):$(TAG) \
		--load .

push: builder
	docker buildx build --builder $(BUILDER) \
		--platform $(PLATFORMS) \
		-t $(IMAGE):$(TAG) \
		--push .

clean:
	docker buildx rm $(BUILDER) 2>/dev/null || true
