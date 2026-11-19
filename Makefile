APP        := notes-api
IMAGE      ?= $(APP):local
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
CHART      := deploy/helm/notes-api
TF_DIR     := infra/terraform/aws
KIND_NAME  ?= notes

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

## ---- App ------------------------------------------------------------------
.PHONY: run
run: ## Run the API locally on :8080
	go run -ldflags "-X main.version=$(VERSION)" ./cmd/server

.PHONY: test
test: ## Run unit tests with race detector and coverage
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

## ---- Containers -----------------------------------------------------------
.PHONY: docker-build
docker-build: ## Build the container image
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

.PHONY: scan
scan: docker-build ## Scan the image with Trivy (HIGH/CRITICAL)
	trivy image --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1 $(IMAGE)

.PHONY: up
up: ## Start API + Prometheus + Grafana with Docker Compose
	docker compose up --build -d
	@echo "API http://localhost:8080 | Prometheus http://localhost:9090 | Grafana http://localhost:3000"

.PHONY: down
down: ## Stop the Compose stack
	docker compose down

## ---- Kubernetes (local kind cluster) --------------------------------------
.PHONY: kind-up
kind-up: ## Create a local kind cluster
	kind create cluster --name $(KIND_NAME)

.PHONY: deploy
deploy: docker-build ## Build, load into kind and install the Helm chart
	kind load docker-image $(IMAGE) --name $(KIND_NAME)
	helm upgrade --install $(APP) $(CHART) -n notes --create-namespace \
		--set image.repository=$(APP) --set image.tag=local --set image.pullPolicy=Never --wait

.PHONY: smoke
smoke: ## Port-forward the service and run the smoke test
	@kubectl -n notes port-forward svc/$(APP) 8080:80 >/dev/null 2>&1 & \
		PF=$$!; sleep 3; ./scripts/smoke-test.sh http://localhost:8080; RC=$$?; kill $$PF; exit $$RC

.PHONY: kind-down
kind-down: ## Delete the local kind cluster
	kind delete cluster --name $(KIND_NAME)

## ---- Infrastructure -------------------------------------------------------
.PHONY: helm-lint
helm-lint: ## Lint the Helm chart
	helm lint --strict $(CHART)

.PHONY: tf-validate
tf-validate: ## Format-check and validate Terraform (no AWS credentials needed)
	terraform -chdir=$(TF_DIR) fmt -check -recursive
	terraform -chdir=$(TF_DIR) init -backend=false -input=false
	terraform -chdir=$(TF_DIR) validate

.PHONY: tf-plan
tf-plan: ## terraform plan (needs AWS credentials)
	terraform -chdir=$(TF_DIR) init
	terraform -chdir=$(TF_DIR) plan -out=tfplan

.PHONY: clean
clean: ## Remove build artifacts
	rm -f coverage.out
