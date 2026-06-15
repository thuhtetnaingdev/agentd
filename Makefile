.PHONY: build run dev install-web build-web clean

# Build the Go binary
build:
	go build -o agentd ./cmd/agentd

# Build web frontend then Go binary (for production)
all: build-web build

# Run in development mode
run:
	go run ./cmd/agentd

# Install web dependencies
install-web:
	cd web && npm install

# Build web frontend to web-dist/
build-web:
	cd web && npm run build
	mkdir -p internal/server/web-dist
	cp -r web/dist/* internal/server/web-dist/

# Clean build artifacts
clean:
	rm -f agentd
	rm -rf internal/server/web-dist
	rm -rf web/dist

# Run tests
test:
	go test ./...

# Run with custom port
dev:
	go run ./cmd/agentd --port 3001 --dir .
