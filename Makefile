#!make
install_golangci_lint=(go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
install_staticcheck=(go install honnef.co/go/tools/cmd/staticcheck@latest)
.PHONY:vet deps lint upgrade
vet:
	@echo "Running go vet"
	@go vet ./...
deps:
	@echo "Downloading..."
	@go mod download -x
lint:vet
	@echo "Running golangci-lint"
	@hash golangci-lint > /dev/null 2>&1 || $(install_golangci_lint)
	@golangci-lint run
	@echo "Running staticcheck"
	@hash staticcheck > /dev/null 2>&1 || $(install_staticcheck)
	@staticcheck ./...
upgrade:
	@echo "Upgrading..."
	@go mod tidy
	@go get -u -v ./...
	@go mod tidy
	@echo "Upgrade complete."