# Makefile for building Tapio base Docker images

REGISTRY ?= ghcr.io/yairfalse
VERSION ?= latest

.PHONY: all
all: base-alpine base-debian runtime-alpine

.PHONY: base-alpine
base-alpine:
	@echo "Building base-alpine image..."
	docker build -f base-alpine.Dockerfile -t $(REGISTRY)/tapio/base-alpine:$(VERSION) .
	docker tag $(REGISTRY)/tapio/base-alpine:$(VERSION) $(REGISTRY)/tapio/base-alpine:latest

.PHONY: base-debian
base-debian:
	@echo "Building base-debian image..."
	docker build -f base-debian.Dockerfile -t $(REGISTRY)/tapio/base-debian:$(VERSION) .
	docker tag $(REGISTRY)/tapio/base-debian:$(VERSION) $(REGISTRY)/tapio/base-debian:latest

.PHONY: runtime-alpine
runtime-alpine:
	@echo "Building runtime-alpine image..."
	docker build -f runtime-alpine.Dockerfile -t $(REGISTRY)/tapio/runtime-alpine:$(VERSION) .
	docker tag $(REGISTRY)/tapio/runtime-alpine:$(VERSION) $(REGISTRY)/tapio/runtime-alpine:latest

.PHONY: push
push: all
	@echo "Pushing images to registry..."
	docker push $(REGISTRY)/tapio/base-alpine:$(VERSION)
	docker push $(REGISTRY)/tapio/base-alpine:latest
	docker push $(REGISTRY)/tapio/base-debian:$(VERSION)
	docker push $(REGISTRY)/tapio/base-debian:latest
	docker push $(REGISTRY)/tapio/runtime-alpine:$(VERSION)
	docker push $(REGISTRY)/tapio/runtime-alpine:latest

.PHONY: clean
clean:
	@echo "Removing local images..."
	docker rmi $(REGISTRY)/tapio/base-alpine:$(VERSION) || true
	docker rmi $(REGISTRY)/tapio/base-alpine:latest || true
	docker rmi $(REGISTRY)/tapio/base-debian:$(VERSION) || true
	docker rmi $(REGISTRY)/tapio/base-debian:latest || true
	docker rmi $(REGISTRY)/tapio/runtime-alpine:$(VERSION) || true
	docker rmi $(REGISTRY)/tapio/runtime-alpine:latest || true